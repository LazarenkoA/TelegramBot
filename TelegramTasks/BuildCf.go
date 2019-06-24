package telegram

import (
	cf "1C/Configuration"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type BuildCf struct {
	BaseTask

	//repName    string
	ChoseRep    *cf.Repository
	versiontRep int
	fileResult  string
	outСhan     chan *struct {
		file    string
		version string
	}
	notInvokeInnerFinish bool
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
	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, msgText)
	B.bot.Send(msg)
	//B.repName = ChoseData

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		/* if B.GetMessage().Text == "отмена" {
			defer B.finish()
			defer func() { B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Отменено")) }()
			return true
		} */

		var version int
		var err error
		if version, err = strconv.Atoi(B.GetMessage().Text); err != nil {
			B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите число."))
			return false
		} else if !B.AllowSaveLastVersion && version == -1 {
			B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Необходимо явно указать версию (на основании номера версии формируется версия в МС)"))
			return false
		} else {
			B.versiontRep = version
		}

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Старт выгрузки версии "+B.GetMessage().Text+". По окончанию будет уведомление.")
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
			B.innerFinish()
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

	var err error
	B.fileResult, err = Cf.SaveConfiguration(B.ChoseRep, B.versiontRep)
	if err != nil {
		panic(err) // в defer перехват
	} else if B.ReadVersion {
		if err := Cf.ReadVervionFromConf(B.fileResult); err != nil {
			logrus.Errorf("Ошибка чтения версии из файла конфигурации:\n %v", err)
		}
	}

	if B.outСhan != nil {
		B.outСhan <- &struct {
			file    string
			version string
		}{file: B.fileResult, version: B.cf.Version}
		close(B.outСhan)
	}

}

func (B *BuildCf) GetCfConf() *cf.ConfCommonData {
	if B.cf == nil {
		B.cf = new(cf.ConfCommonData)
	}

	return B.cf
}

func (B *BuildCf) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.state = StateWork
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.AppendDescription(B.name)
	B.startInitialise(bot, update, finish)

}

func (B *BuildCf) startInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите хранилище")
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
	bot.Send(msg)
}

func (B *BuildCf) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}

	Msg := fmt.Sprintf("Конфигурация версии %v выгружена из %v. Файл %v", B.versiontRep, B.ChoseRep.Name, B.fileResult)
	B.baseFinishMsg(Msg)
}

func (B *BuildCf) GetCallBack() map[string]func() {
	return B.callback
}
