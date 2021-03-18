package telegram

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"strings"

	cf "github.com/LazarenkoA/TelegramBot/Configuration"

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
	ChoseRep   *cf.Repository
	versionRep int
	fileResult string
	ReadVersion bool
	cf          *cf.ConfCommonData
}

func (B *BuildCf) ProcessChose(ChoseData string) {
	B.gotoByName("BuildCf-2")

	for _, rep := range Confs.RepositoryConf {
		if rep.Name == ChoseData {
			B.ChoseRep = rep
			break
		}
	}

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		var version int
		var err error
		if version, err = strconv.Atoi(strings.Trim(B.GetMessage().Text, " ")); err != nil {
			// Прыгнуть нужно на предпоследний шаг
			B.DeleteMsg(update.Message.MessageID)
			B.gotoByName("BuildCf-2", fmt.Sprintf("Введите число. Вы ввели %q", B.GetMessage().Text), B.steps[1].(*step).Msg)
			return false
		} else {
			B.versionRep = version
			B.DeleteMsg(update.Message.MessageID)
			//B.steps[len(B.steps)-1].(*step).Msg = B.steps[len(B.steps)-2].(*step).Msg
			B.gotoByName("BuildCf-3", "⚙️ Старт выгрузки версии "+B.GetMessage().Text+". По окончанию будет уведомление.", B.steps[1].(*step).Msg)
		}

		go B.Invoke()
		return true
	}
}

func (B *BuildCf) Invoke() {
	defer func() {
		if err := recover(); err != nil {
			logrus.WithField("Версия хранилища", B.versionRep).WithField("Имя репозитория", B.ChoseRep.Name).Errorf("Произошла ошибка при сохранении конфигурации: %w", err)
			msg := fmt.Sprintf("Произошла ошибка при сохранении конфигурации %q (версия %v): %v", B.ChoseRep.Name, B.versionRep, err)
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, msg))
			B.invokeEndTask(reflect.TypeOf(B).String())
		} else {
			// вызываем события
			for _, f := range B.AfterBuild {
				f()
			}
		}
	}()

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

	if B.versionRep == -1 {
		Report, err := Cf.SaveReport(B.ChoseRep, -1, 0)
		if err != nil {
			logrus.WithError(err).Panic("Не удалось получить отчет по хранилищу конфигурации")
		}

		parcedRep, err := Cf.GetReport(Report)
		if err != nil || len(parcedRep) == 0 {
			logrus.WithError(err).Panic("Не удалось получить последнюю версию из отчета по хранилищу конфигурации")
		}

		B.versionRep = parcedRep[0].Version
	}

	var err error
	B.fileResult, err = Cf.SaveConfiguration(B.ChoseRep, B.versionRep)
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
		new(step).Construct("Введите версию хранилища для выгрузки (если указать -1, будет сохранена последняя версия).", "BuildCf-2", B, ButtonBack|ButtonCancel, 2),
		new(step).Construct("Готово", "BuildCf-3", B, 0, 2),
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
