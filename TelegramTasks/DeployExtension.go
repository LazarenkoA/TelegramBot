package telegram

import (
	cf "1C/Configuration"
	git "1C/Git"
	"1C/fresh"
	JK "1C/jenkins"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type EventDeployExtension struct {
	EndTask []func()
}

type DeployExtension struct {
	BuilAndUploadCfe
	EventDeployExtension

	git    *git.Git
	baseSM *Bases
	once   sync.Once
	fresh  *fresh.Fresh
}

func (this *DeployExtension) GetBaseSM() (result *Bases, err error) {
	// once нужен для первой инициализации
	this.once.Do(func() {
		var SMName string = "sm"
		errors := []error{}
		if this.fresh.Conf == nil { // Значение уже может быть инициализировано (из потомка)
			this.fresh.Conf = this.freshConf
		}
		var Allbases = []*Bases{}
		this.JsonUnmarshal(this.fresh.GetDatabase(), &Allbases)

		// Находим МС
		for _, DB := range Allbases {
			if strings.ToLower(DB.Name) == SMName {
				this.baseSM = DB
				break
			}
		}

		if this.baseSM.UUID == "" {
			errors = append(errors, fmt.Errorf("База %q не найдена", SMName))
		}
		if this.baseSM.UUID != "" && this.baseSM.UserName == "" {
			errors = append(errors, fmt.Errorf("У базы %q не задана учетная запись администратора", SMName))
		}
		if this.baseSM.UUID != "" && this.baseSM.UserPass == "" {
			errors = append(errors, fmt.Errorf("У базы %q не задан пароль учетной записи администратора", SMName))
		}
		if len(errors) > 0 {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Произошли ошибки:"))
			for _, err := range errors {
				logrus.Error(err)
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, err.Error()))

			}
			err = fmt.Errorf("Не удалось получить базу МС.")
			result = nil
		} else {
			err = nil
			result = this.baseSM
		}

	})

	return result, err
}

func (this *DeployExtension) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	mutex := new(sync.Mutex)
	this.fresh = new(fresh.Fresh)
	this.EndTask = append(this.EndTask, this.innerFinish)
	this.callback = make(map[string]func())

	this.AfterUploadFresh = append(this.AfterUploadFresh, func(ext cf.IConfiguration) {
		logrus.Debugf("Инкрементируем версию расширения %q", ext.GetName())
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Меняем версию расшерения"))

		branchName := "Dev"
		this.git = new(git.Git)
		this.git.RepDir, _ = filepath.Split(ext.(*cf.Extension).ConfigurationFile)
		this.git.Pull(branchName)

		if err := ext.IncVersion(); err != nil {
			logrus.WithField("Расширение", ext.GetName()).Error(err)
		} else {
			this.CommitAndPush(ext.(*cf.Extension).ConfigurationFile, branchName, mutex)
		}

		msg := tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Отправляем задание в jenkins по расширению %q. \nУстановить монопольно?", ext.GetName()))
		Buttons := make([]map[string]interface{}, 0)
		this.appendButton(&Buttons, "Да", func() {
			if err := this.InvokeJobJenkins(ext, true); err == nil {
				bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
			} else {
				bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
			}
		})
		this.appendButton(&Buttons, "Нет", func() {
			if err := this.InvokeJobJenkins(ext, false); err == nil {
				bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
			} else {
				bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
			}
		})

		this.createButtons(&msg, Buttons, 3, true)
		bot.Send(msg)

	})

	this.AppendDescription(this.name)
	return this
}

func (this *DeployExtension) Start() {
	this.BuilAndUploadCfe.Initialise(this.bot, this.update, this.outFinish)
	// у предка переопределяем события окончания выполнения, что бы оно не отработало раньше времени
	this.BuilAndUploadCfe.EndTask = []func(){}
	this.BuilAndUploadCfe.Start() // метод предка
}

func (this *DeployExtension) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

// GIT
func (this *DeployExtension) CommitAndPush(filePath, branchName string, mutex *sync.Mutex) {
	logrus.Debug("Коммитим версию в хранилище")

	if this.git.BranchExist(branchName) {
		// критическая секция, коммиты должны происходить последовательно, не паралельно
		mutex.Lock()
		func() {
			defer mutex.Unlock()
			if err := this.git.CommitAndPush(branchName, filePath, "Автоинкремент версии"); err != nil {
				logrus.Errorf("Ошибка при коммите измененной версии: %v", err)
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Ошибка при коммите измененной версии: %v", err)))
			}
		}()
	} else {
		logrus.WithField("Ветка", branchName).Error("Ветка не существует")
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Ветка %q не существует", branchName)))
	}
}

//Jenkins
func (this *DeployExtension) InvokeJobJenkins(ext cf.IConfiguration, exclusive bool) (err error) {
	defer func() {
		if e := recover(); e != nil {
			logrus.Error(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, e))
			this.innerFinish()
			err = fmt.Errorf("Ошибка при отправки в Jenkins: %v", e)
		}
	}()

	baseSM, err := this.GetBaseSM()
	if err != nil {
		logrus.Panic("Ошибка получения базы МС")
	}

	Availablebases := []Bases{}
	this.JsonUnmarshal(this.fresh.GetAvailableDatabase(ext.GetName()), &Availablebases)

	result := map[string]int{
		"error":   0,
		"success": 0,
	}

	jk := new(JK.Jenkins).Create("update-cfe")
	jk.RootURL = Confs.Jenkins.URL
	jk.User = Confs.Jenkins.Login
	jk.Pass = Confs.Jenkins.Password
	jk.Token = Confs.Jenkins.UserToken

	for _, DB := range Availablebases {
		err = jk.InvokeJob(map[string]string{
			"srv":        DB.Cluster.MainServer,
			"db":         DB.Name,
			"ras_srv":    DB.Cluster.RASServer,
			"ras_port":   fmt.Sprintf("%d", DB.Cluster.RASPort),
			"usr":        DB.UserName,
			"pwd":        DB.UserPass,
			"cfe_name":   ext.GetName(),
			"cfe_id":     ext.(*cf.Extension).GUID,
			"kill_users": strconv.FormatBool(exclusive),
			"SM_URL":     baseSM.URL,
			"SM_USR":     baseSM.UserName,
			"SM_PWD":     baseSM.UserPass,
		})
		if err != nil {
			logrus.Errorf("Ошибка при отправки задания update-cfe: %v", err)
			result["error"]++
		} else {
			result["success"]++
		}
	}

	msg := fmt.Sprintf("Задания успешно отправлены для %d баз", result["success"])
	if result["error"] > 0 {
		msg += fmt.Sprintf("\nДля %d баз произошла ошибка при отправки", result["error"])
	}
	this.bot.Send(tgbotapi.NewMessage(this.ChatID, msg))

	end := func() {
		// вызываем события окончания выполнения текущего задание
		for _, f := range this.EndTask {
			f()
		}
	}

	// Отслеживаем статус
	go jk.CheckStatus(
		func() {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"update-cfe\" выполнено успешно."))
			end()
		},
		func() {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Выполнение задания \"update-cfe\" завершилось с ошибкой"))
			end()
		},
		func() {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"update-cfe\" не удалось определить статус, прервано по таймауту"))
			end()
		},
	)
	return nil
}

func (B *DeployExtension) InfoWrapper(task ITask) {
	B.info = "ℹ Команда выгружает файл конфигурации (*.cfe)\n" +
		"Отправляет его в менеджер сервиса\n" +
		"Инкрементирует версию расширения в ветке Dev\n" +
		"Инициирует обновление в jenkins."
	B.BaseTask.InfoWrapper(task)
}
