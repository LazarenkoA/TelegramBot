package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type Data struct {
	Task  string `json:"Task"`
	Error bool   `json:"Error"`
	State string `json:"State"`
	UUID  string `json:"UUID"`
}

type GetListUpdateState struct {
	BaseTask

	date                 time.Time
	freshConf            *cf.FreshConf
	notInvokeInnerFinish bool
}

func (B *GetListUpdateState) ChoseMC(ChoseData string) {
	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	B.getData()
}

func (B *GetListUpdateState) ChoseNo(ChoseData string) {
	B.notInvokeInnerFinish = false
	B.outFinish()
	B.innerFinish()
}

func (B *GetListUpdateState) ChoseYes(ChoseData string) {
	B.date = B.date.AddDate(0, 0, -1)
	B.getData()
}

func (B *GetListUpdateState) getData() {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		} else {
			B.innerFinish()
			B.outFinish()
		}
	}()

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetListUpdateState(B.date.Format("20060102"))
	var data = []Data{}

	B.JsonUnmarshal(JSON, &data)

	if len(data) == 0 {
		B.notInvokeInnerFinish = true
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("За дату %v нет данных", B.date.Format("02.01.2006")))
		keyboard := tgbotapi.InlineKeyboardMarkup{}
		var Buttons = []tgbotapi.InlineKeyboardButton{}

		B.callback = make(map[string]func(ChoseData string), 0)
		btn := tgbotapi.NewInlineKeyboardButtonData("Запросить данные за -1 день", "yes")
		btn2 := tgbotapi.NewInlineKeyboardButtonData("Отмена", "no")
		B.callback["yes"] = B.ChoseYes
		B.callback["no"] = B.ChoseNo
		Buttons = append(Buttons, btn, btn2)

		keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
		msg.ReplyMarkup = &keyboard
		B.bot.Send(msg)
	} else {
		B.notInvokeInnerFinish = false
		for _, line := range data {
			Msg := fmt.Sprintf("Дата: %v\nЗадание: %v\nСтатус: %q", B.date.Format("02.01.2006"), line.Task, line.State)
			B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, Msg))
		}
	}
}

func (B *GetListUpdateState) StartInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.date = time.Now()

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите агент сервиса")
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	B.callback = make(map[string]func(ChoseData string), 0)
	for _, conffresh := range Confs.FreshConf {
		btn := tgbotapi.NewInlineKeyboardButtonData(conffresh.Alias, conffresh.Name)
		B.callback[conffresh.Name] = B.ChoseMC
		Buttons = append(Buttons, btn)
	}

	keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
	msg.ReplyMarkup = &keyboard
	bot.Send(msg)

	/* B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Загружаем конфигурацию %q в МС", fileName)))

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выберите менеджер сервиса для загрузки расширений")
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	B.callback = make(map[string]func(ChoseData string), 0)
	for _, conffresh := range Confs.FreshConf {
		btn := tgbotapi.NewInlineKeyboardButtonData(conffresh.Alias, conffresh.Name)
		B.callback[conffresh.Name] = B.ChoseMC
		Buttons = append(Buttons, btn)
	}

	keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
	msg.ReplyMarkup = &keyboard
	bot.Send(msg) */
}

func (B *GetListUpdateState) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}

	B.baseFinishMsg("Готово!")
}
