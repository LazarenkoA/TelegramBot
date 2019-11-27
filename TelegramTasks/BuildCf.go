package telegram

import (
	cf "1C/Configuration"
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type EventBuildCf struct {
	BeforeBuild []func()
	AfterBuild  []func()
}

type BuildCf struct {
	BaseTask
	EventBuildCf

	//repName    string
	ChoseRep    *cf.Repository
	versiontRep int
	fileResult  string

	// Флаг разрешает сохранять версию с указанием -1 (HEAD)
	AllowSaveLastVersion bool
	ReadVersion          bool
	cf                   *cf.ConfCommonData
}

func (B *BuildCf) ProcessChose(ChoseData string) {
	var addMsg string
	if B.AllowSaveLastVersion {
		addMsg = " (если указать -1, будет сохранена последняя версия)"
	}
	msgText := fmt.Sprintf("Введите версию хранилища для выгрузки%v.", addMsg)
	B.next(msgText)

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		var version int
		var err error
		if version, err = strconv.Atoi(strings.Trim(B.GetMessage().Text, " ")); err != nil {
			// Прыгнуть нужно на предпоследний шаг
			B.DeleteMsg(update.Message.MessageID)
			B.goTo(len(B.steps)-2, fmt.Sprintf("Введите число. Вы ввели %q", B.GetMessage().Text))
			return false
		} else if !B.AllowSaveLastVersion && version == -1 {
			B.DeleteMsg(update.Message.MessageID)
			B.goTo(len(B.steps)-2, "Необходимо явно указать версию (на основании номера версии формируется версия в МС)")
			return false
		} else {
			B.versiontRep = version
			B.DeleteMsg(update.Message.MessageID)
			B.steps[len(B.steps)-1].(*step).Msg = B.steps[len(B.steps)-2].(*step).Msg
			B.goTo(len(B.steps)-1, "⚙️ Старт выгрузки версии "+B.GetMessage().Text+". По окончанию будет уведомление.")
		}

		go B.Invoke(ChoseData)
		return true
	}
}

func (B *BuildCf) Invoke(repName string) {
	defer func() {
		if err := recover(); err != nil {
			logrus.WithField("Версия хранилища", B.versiontRep).WithField("Имя репозитория", B.ChoseRep.Name).Errorf("Произошла ошибка при сохранении конфигурации: %v", err)
			msg := fmt.Sprintf("Произошла ошибка при сохранении конфигурации %q (версия %v): %v", B.ChoseRep.Name, B.versiontRep, err)
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, msg))
			B.invokeEndTask(reflect.TypeOf(B).String())
		} else {
			// вызываем события
			for _, f := range B.AfterBuild {
				f()
			}
		}
	}()
	for _, rep := range Confs.RepositoryConf {
		if rep.Name == repName {
			B.ChoseRep = rep
			break
		}
	}

	Cf := B.GetCfConf()
	if Cf.BinPath == "" {
		Cf.BinPath = Confs.BinPath
	}
	if Cf.OutDir == "" {
		Cf.OutDir = Confs.OutDir
	}

	// вызываем события
	for _, f := range B.BeforeBuild {
		f()
	}

	var err error
	B.fileResult, err = Cf.SaveConfiguration(B.ChoseRep, B.versiontRep)
	if err != nil {
		panic(err) // в defer перехват
	} else if B.ReadVersion {
		if err := Cf.ReadVervionFromConf(B.fileResult); err != nil {
			logrus.Errorf("Ошибка чтения версии из файла конфигурации:\n %v", err)
		}
	}
}

func (B *BuildCf) GetCfConf() *cf.ConfCommonData {
	if B.cf == nil {
		B.cf = new(cf.ConfCommonData)
	}

	return B.cf
}

func (B *BuildCf) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)
	B.EndTask[reflect.TypeOf(B).String()] = []func(){finish}
	B.AfterBuild = append(B.AfterBuild, func() {
		B.bot.Send(tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Конфигурация сохранена %v", B.fileResult)))
		B.invokeEndTask(reflect.TypeOf(B).String())
	})

	firstStep := new(step).Construct("Выберите конфигурацию", "BuildCf-1", B, ButtonCancel|ButtonBack, 3)
	for _, rep := range Confs.RepositoryConf {
		Name := rep.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(rep.Alias, func() { B.ProcessChose(Name) })
	}
	firstStep.reverseButton()

	B.steps = []IStep{
		firstStep,
		new(step).Construct("", "BuildCf-2", B, ButtonBack|ButtonCancel, 2),
		new(step).Construct("", "BuildCf-3", B, 0, 2),
	}

	B.AppendDescription(B.name)
	return B
}

func (B *BuildCf) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	B.steps[B.currentStep].invoke(&B.BaseTask)
}

func (B *BuildCf) InfoWrapper(task ITask) {
	OutDir := Confs.OutDir
	if strings.Trim(OutDir, "") == "" {
		OutDir, _ = ioutil.TempDir("", "")
	}
	B.info = fmt.Sprintf("ℹ Команда выгружает файл конфигурации (*.cf), файл сохраняется на диске в каталог %v.", OutDir)
	B.BaseTask.InfoWrapper(task)
}

func (B *BuildCf) GetCallBack() map[string]func() {
	return B.callback
}
