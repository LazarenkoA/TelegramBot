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
	ChoseRep             *cf.Repository
	version              int
	fileResult           string
	outСhan              chan string
	notInvokeInnerFinish bool
}

func (B *BuildCf) ProcessChose(ChoseData string) {
	B.state = StateWork

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите версию для выгрузки (если указать -1, будет сохранена последняя версия). Для отмены воспользуйтесь командой /Cancel")
	B.bot.Send(msg)
	//B.repName = ChoseData

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		/* if update.Message.Text == "отмена" {
			defer B.finish()
			defer func() { B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Отменено")) }()
			return true
		} */

		var version int
		var err error
		if version, err = strconv.Atoi(update.Message.Text); err != nil {
			msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите число или воспользуйтесь командой /Cancel")
			B.bot.Send(msg)
			return false
		} else {
			B.version = version
		}

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Старт выгрузки версии "+update.Message.Text+". По окончанию будет уведомление.")
		B.bot.Send(msg)

		go B.Invoke(ChoseData)
		return true
	}
}

func (B *BuildCf) Invoke(repName string) {
	defer func() {
		if err := recover(); err != nil {
			logrus.WithField("Версия конфигурации", B.version).WithField("Имя репозитория", B.ChoseRep.Name).Errorf("Произошла ошибка при сохранении конфигурации: %v", err)
			Msg := fmt.Sprintf("Произошла ошибка при сохранении конфигурации %q (версия %v): %v", B.ChoseRep.Name, B.version, err)
			B.baseFinishMsg(Msg)
		} else {
			B.innerFinish()
			B.outFinish()
		}
	}()
	for _, rep := range Confs.RepositoryConf {
		if rep.Name == repName {
			B.ChoseRep = rep
			break
		}
	}

	conf := new(cf.ConfCommonData)
	conf.BinPath = Confs.BinPath

	var err error
	B.fileResult, err = conf.SaveConfiguration(B.ChoseRep, B.version)
	if err != nil {
		panic(err) // в defer перехват
	}

	if B.outСhan != nil {
		B.outСhan <- B.fileResult
		close(B.outСhan)
	}

}

func (B *BuildCf) StartInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите хранилище")
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	B.callback = make(map[string]func(ChoseData string), 0)
	for _, rep := range Confs.RepositoryConf {
		btn := tgbotapi.NewInlineKeyboardButtonData(rep.Alias, rep.Name)
		B.callback[rep.Name] = B.ProcessChose
		Buttons = append(Buttons, btn)
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

	keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
	msg.ReplyMarkup = &keyboard
	bot.Send(msg)
}

func (B *BuildCf) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}

	Msg := fmt.Sprintf("Конфигурация версии %v выгружена из %v. Файл %v", B.version, B.ChoseRep.Name, B.fileResult)
	B.baseFinishMsg(Msg)
}
