package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type Bases struct {
	Name string `json:"Name"`
	UUID string `json:"UUID"`
}

type Updates struct {
	Name string `json:"Name"`
	UUID string `json:"UUID"`
}

type SetPlanUpdate struct {
	BaseTask

	freshConf *cf.FreshConf
	UUIDBase  string
	//UpdateUUID string
}

func (B *SetPlanUpdate) ChoseUpdate(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		}
	}()

	if B.freshConf == nil {
		panic("Не определены настройки для МС")
	}

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Укажите через сколько минут необходимо запустить обновление. Для отмены воспользуйтесь командой /Cancel")
	B.bot.Send(msg)

	B.hookInResponse = func(update *tgbotapi.Update) (result bool) {
		defer func() {
			if err := recover(); err != nil {
				Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(Msg)
				B.baseFinishMsg(Msg)
				result = true
			} else {
				if result {
					B.innerFinish()
					B.outFinish()
				}
			}
		}()

		if MinuteShift, err := strconv.Atoi(B.GetMessage().Text); err != nil {
			msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите число или воспользуйтесь командой /Cancel")
			B.bot.Send(msg)
			result = false
		} else {
			fresh := new(fresh.Fresh)
			fresh.Conf = B.freshConf
			if e := fresh.SetUpdetes(ChoseData, B.UUIDBase, MinuteShift, nil); e != nil {
				result = false
				panic(e) // в defer перехват
			}
		}

		result = true
		return
	}
}

func (B *SetPlanUpdate) ChoseBD(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		}
	}()

	if B.freshConf == nil {
		panic("Не определены настройки для МС")
	}

	B.UUIDBase = ChoseData

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetAvailableUpdates(B.UUIDBase)
	var updates = []Updates{}

	B.JsonUnmarshal(JSON, &updates)
	if len(updates) != 0 {
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите обновление")
		keyboard := tgbotapi.InlineKeyboardMarkup{}
		var Buttons = []tgbotapi.InlineKeyboardButton{}
		B.callback = make(map[string]func(ChoseData string), 0)

		for _, line := range updates {
			//msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("База %q", line.Name))
			btn := tgbotapi.NewInlineKeyboardButtonData(line.Name, line.UUID)
			B.callback[line.UUID] = B.ChoseUpdate
			Buttons = append(Buttons, btn)
		}

		keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 3)
		msg.ReplyMarkup = &keyboard
		B.bot.Send(msg)
	} else {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Доступных обновлений не найдено"))
	}
}

func (B *SetPlanUpdate) ChoseMC(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		}
	}()

	//B.state = StateWork

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetDatabase()
	var bases = []Bases{}

	B.JsonUnmarshal(JSON, &bases)
	if len(bases) != 0 {
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите базу")
		keyboard := tgbotapi.InlineKeyboardMarkup{}
		var Buttons = []tgbotapi.InlineKeyboardButton{}
		B.callback = make(map[string]func(ChoseData string), 0)

		for _, line := range bases {
			//msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("База %q", line.Name))
			btn := tgbotapi.NewInlineKeyboardButtonData(line.Name, line.UUID)
			B.callback[line.UUID] = B.ChoseBD
			Buttons = append(Buttons, btn)
		}

		keyboard.InlineKeyboard = breakButtonsByColum(Buttons, 1)
		msg.ReplyMarkup = &keyboard
		B.bot.Send(msg)
	} else {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Баз не найдено"))
	}

}

func (B *SetPlanUpdate) StartInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите менеджер сервиса для загрузки конфигурации")
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
}

func (B *SetPlanUpdate) innerFinish() {
	B.baseFinishMsg("Готово!")
}
