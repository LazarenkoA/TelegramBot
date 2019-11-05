package telegram

import (
	cf "1C/Configuration"
	"fmt"
	"io/ioutil"
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
	B.state = StateWork

	var addMsg string
	if B.AllowSaveLastVersion {
		addMsg = " (если указать -1, будет сохранена последняя версия)"
	}
	msgText := fmt.Sprintf("Введите версию хранилища для выгрузки%v.", addMsg)
	msg := tgbotapi.NewMessage(B.ChatID, msgText)
	B.bot.Send(msg)

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		var version int
		var err error
		if version, err = strconv.Atoi(strings.Trim(B.GetMessage().Text, " ")); err != nil {
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Введите число. Вы ввели %q", B.GetMessage().Text)))
			return false
		} else if !B.AllowSaveLastVersion && version == -1 {
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Необходимо явно указать версию (на основании номера версии формируется версия в МС)"))
			return false
		} else {
			B.versiontRep = version
		}

		msg := tgbotapi.NewMessage(B.ChatID, "Старт выгрузки версии "+B.GetMessage().Text+". По окончанию будет уведомление.")
		B.bot.Send(msg)

		B.AppendDescription(fmt.Sprintf("Выгрузка версии %v", version))
		go B.Invoke(ChoseData)
		return true
	}
}

func (B *BuildCf) Invoke(repName string) {
	defer func() {
		if err := recover(); err != nil {
			logrus.WithField("Версия хранилища", B.versiontRep).WithField("Имя репозитория", B.ChoseRep.Name).Errorf("Произошла ошибка при сохранении конфигурации: %v", err)
			Msg := fmt.Sprintf("Произошла ошибка при сохранении конфигурации %q (версия %v): %v", B.ChoseRep.Name, B.versiontRep, err)
			B.baseFinishMsg(Msg)
		} else {
			// вызываем события
			for _, f := range B.AfterBuild {
				f()
			}
		}
		B.outFinish()
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
	B.AfterBuild = append(B.AfterBuild, B.innerFinish)

	B.AppendDescription(B.name)
	return B
}

func (B *BuildCf) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	msg := tgbotapi.NewMessage(B.ChatID, "Выберите хранилище")
	B.callback = make(map[string]func())
	Buttons := make([]map[string]interface{}, 0)

	for _, rep := range Confs.RepositoryConf {
		Name := rep.Name // Обязательно через переменную, нужно для замыкания
		B.appendButton(&Buttons, rep.Alias, func() { B.ProcessChose(Name) })
	}

	/* numericKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("1"),
			tgbotapi.NewKeyboardButton("2"),
			tgbotapi.NewKeyboardButton("3"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("4"),
			tgbotapi.NewKeyboardButton("5"),
			tgbotapi.NewKeyboardButton("6"),
		),
	) */

	B.createButtons(&msg, Buttons, 3, true)
	B.bot.Send(msg)
}

func (B *BuildCf) InfoWrapper(task ITask) {
	OutDir := Confs.OutDir
	if strings.Trim(OutDir, "") == "" {
		OutDir, _ = ioutil.TempDir("", "")
	}
	B.info = fmt.Sprintf("ℹ Команда выгружает файл конфигурации (*.cf), файл сохраняется на диске в каталог %v.", OutDir)
	B.BaseTask.InfoWrapper(task)
}

func (B *BuildCf) innerFinish() {
	Msg := fmt.Sprintf("Конфигурация версии %v выгружена из %v. Файл %v", B.versiontRep, B.ChoseRep.Name, B.fileResult)
	B.baseFinishMsg(Msg)
}

func (B *BuildCf) GetCallBack() map[string]func() {
	return B.callback
}
