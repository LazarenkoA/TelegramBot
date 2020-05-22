package telegram

import (
	cf "TelegramBot/Configuration"
	conf "TelegramBot/Configuration"
	git "TelegramBot/Git"
	"TelegramBot/fresh"
	JK "TelegramBot/jenkins"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type EventDeployExtension struct {
}

type DeployExtension struct {
	BuilAndUploadCfe
	EventDeployExtension

	git          *git.Git
	baseSM *Bases
	availablebases map[string]Bases
	once         sync.Once
	fresh        *fresh.Fresh
	extentions   []*conf.Extension
	countExt     int
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
			this.baseSM = nil
		} else {
			err = nil
		}

	})

	return this.baseSM, err
}

func (this *DeployExtension) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	logrus.Debug("Инициализация DeployExtension")

	muGit := new(sync.Mutex) // для работы с гитом, как коммитить параллельно если деплоим несколько расширений
	//mutex := new(sync.Mutex) // что бы сообщения выдавались один за другим, в первом нажали кнопку, появилось второе, а не куча сразу

	//this.BuildCfe.HideAllButtun = true // важно до инициализации
	this.ChosedBranch = "Dev" // вот такой хардкод :Р (важно до инициализации)
	this.extentions = []*conf.Extension{}
	this.BuilAndUploadCfe.Initialise(bot, update, finish)

	this.EndTask = make(map[string][]func(), 0)
	this.EndTask[reflect.TypeOf(this).String()] = []func(){finish}
	this.BeforeBuild = append(this.BeforeBuild, func(ext cf.IConfiguration) {
		logrus.Debugf("Инкрементируем версию расширения %q", ext.GetName())
		this.bot.Send(tgbotapi.NewEditMessageText(this.ChatID, this.statusMessageID, fmt.Sprintf("Инкрементируем версию расширения %q", ext.GetName())))

		muGit.Lock()
		func() {
			defer muGit.Unlock()

			this.git = new(git.Git)
			this.git.RepDir, _ = filepath.Split(ext.(*cf.Extension).ConfigurationFile)
			this.git.Pull(this.ChosedBranch)

			if err := ext.IncVersion(); err != nil {
				logrus.WithField("Расширение", ext.GetName()).Error(err)
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при инкременте версии:\n %v", err)))
				return
			} else {
				if err := this.CommitAndPush(ext.(*cf.Extension).ConfigurationFile, this.ChosedBranch); err == nil {
					this.extentions = append(this.extentions, ext.(*conf.Extension))
				}
			}
		}()

		//mutex.Lock()

	})

	this.fresh = new(fresh.Fresh)
	this.AfterAllUploadFresh = append(this.AfterAllUploadFresh, func() {
		this.goTo(len(this.steps)-2, fmt.Sprintf("Отправляем расширения (%d штук) в jenkins, установить монопольно?", len(this.extentions))) // Отправляем задание в jenkins
	})

	// в основном все шаги наследуются от BuilAndUploadCfe, только парочку добавляем новых
	this.steps = append(this.steps,
		new(step).Construct("Отправляем задание в jenkins, установить монопольно?", "DeployExtension-1", this, ButtonCancel, 2).
			appendButton("Да", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, true); err == nil {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.next(status)
				time.Sleep(time.Second * 2) // Спим что бы можно было прочитать во сколько баз было отправлено, или если была ошибка

				//	mutex.Unlock()
			}).
			appendButton("Нет", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, false); err == nil {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.next(status)
				time.Sleep(time.Second * 2) // Спим что бы можно было прочитать во сколько баз было отправлено, или если была ошибка
				//mutex.Unlock()
			}).reverseButton().
			whenGoing(func(thisStep IStep) {
				thisStep.(*step).Msg = this.steps[this.currentStep-1].(*step).Msg // берем msg от предыдущего шага
			}),
		new(step).Construct("", "DeployExtension-2", this, 0, 2),
	)

	this.AppendDescription(this.name)
	return this
}

func (this *DeployExtension) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")

	this.steps[this.currentStep].invoke(&this.BaseTask)

	// у предка переопределяем события окончания выполнения, что бы оно не отработало раньше времени
	//this.BuilAndUploadCfe.Start() // метод предка
}

// GIT
func (this *DeployExtension) CommitAndPush(filePath, branchName string) (err error) {
	logrus.Debug("Коммитим версию в хранилище")

	if this.git.BranchExist(branchName) {
		if err = this.git.CommitAndPush(branchName, filePath, "Автоинкремент версии"); err != nil {
			logrus.Errorf("Ошибка при коммите измененной версии: %v", err)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Ошибка при коммите измененной версии: %v", err)))
		}

	} else {
		err = fmt.Errorf("Ветка %q не существует", branchName)
		logrus.WithField("Ветка", branchName).WithError(err).Error()
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Ветка %q не существует", branchName)))
	}

	return err
}

//Jenkins
func (this *DeployExtension) InvokeJobJenkins(status *string, exclusive bool) (err error) {
	defer func() {
		if e := recover(); e != nil {
			logrus.Error(fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, e))
			err = fmt.Errorf("Ошибка при отправки в Jenkins: %v", e)
			//this.invokeEndTask(reflect.TypeOf(this).String()) мешает при массовом деплои
		}
	}()

	baseSM, err := this.GetBaseSM()
	if err != nil {
		logrus.Panic("Ошибка получения базы МС")
	}

	tmpExt := []map[string]string{}
	for _, ext := range this.extentions {
		if ext.GetID() == "" {
			continue
		}

		if  len(this.availablebases) == 0 {
			bases := []Bases{}
			if err := this.JsonUnmarshal(this.fresh.GetDatabaseByExtension(ext.GetName()), &bases); err == nil {
				for _, b := range bases {
					if _, exist := this.availablebases[b.UUID]; !exist {
						this.availablebases[b.UUID] = b
					}
				}
			}
		}

		tmpExt = append(tmpExt, map[string]string{
			"Name": ext.GetName(),
			"GUID": ext.GetID(),
		})
	}
	byteExtList, _ := json.Marshal(tmpExt)

	result := map[string]int{
		"error":   0,
		"success": 0,
	}

	jk := new(JK.Jenkins).Create("update-cfe")
	jk.RootURL = Confs.Jenkins.URL
	jk.User = Confs.Jenkins.Login
	jk.Pass = Confs.Jenkins.Password
	jk.Token = Confs.Jenkins.UserToken

	for _, DB := range this.availablebases {
		err = jk.InvokeJob(map[string]string{
			"srv":        DB.Cluster.MainServer,
			"db":         DB.Name,
			"ras_srv":    DB.Cluster.RASServer,
			"ras_port":   fmt.Sprintf("%d", DB.Cluster.RASPort),
			"usr":        strings.Trim(DB.UserName, " "),
			"pwd":        DB.UserPass,
			"extList":    string(byteExtList),
			"kill_users": strconv.FormatBool(exclusive),
			"SM_URL":     baseSM.URL,
			"SM_USR":     strings.Trim(baseSM.UserName, " "),
			"SM_PWD":     baseSM.UserPass,
			"jobID":      jk.JobID,
		})
		if err != nil {
			logrus.Errorf("Ошибка при отправки задания update-cfe: %v", err)
			result["error"]++
		} else {
			result["success"]++
		}
	}

	*status = fmt.Sprintf("Задания успешно отправлены для %d баз", result["success"])
	if result["error"] > 0 {
		*status += fmt.Sprintf("\nДля %d баз произошла ошибка при отправки", result["error"])
	}

	// Отслеживаем статус
	go jk.CheckStatus(
		func() {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"update-cfe\" выполнено успешно."))
			this.invokeEndTask(reflect.TypeOf(this).String())
		},
		func() {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"update-cfe\" не удалось определить статус, прервано по таймауту"))
			this.invokeEndTask(reflect.TypeOf(this).String())
		},
		func(err string) {
			msg := tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Выполнение задания \"<b>update-cfe</b>\" завершилось с ошибкой:\n<pre>%v</pre>", err))
			msg.ParseMode = "HTML"
			this.bot.Send(msg)
			this.invokeEndTask(reflect.TypeOf(this).String())
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
