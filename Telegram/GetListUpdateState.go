package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/sirupsen/logrus"
)

type Data struct {
	Task  string `json:"Task"`
	Error bool   `json:"Error"`
	State string `json:"State"`
	UUID  string `json:"UUID"`
	End   bool   `json:"End"`
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

/* func (B *GetListUpdateState) ChoseNo() {
	B.notInvokeInnerFinish = false
	B.outFinish()
	B.innerFinish()
} */

func (B *GetListUpdateState) ChoseYes() {
	B.date = B.date.AddDate(0, 0, -1)
	B.getData()
}

func (B *GetListUpdateState) Cancel() {
	B.innerFinish()
	B.outFinish()
}

func (B *GetListUpdateState) MonitoringState(UUID string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при получении состояние задания: %v", err)
			logrus.Error(Msg)
		}
	}()

	Msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "При изменении состояния будет уведомление")
	B.bot.Send(Msg)

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	var data = new(Data)

	if err, JSON := fresh.GeUpdateState(UUID); err == nil {
		B.JsonUnmarshal(JSON, &data)
	} else {
		panic(err)
	}

	timer := time.NewTicker(time.Minute)
	go func() {
		var Locdata = new(Data)

		for range timer.C {
			if err, JSON := fresh.GeUpdateState(UUID); err == nil {
				B.JsonUnmarshal(JSON, &Locdata)
				if Locdata.State != data.State {
					MsgTxt := fmt.Sprintf("Дата: %v\nЗадание: %v\nСтатус: %q", B.date.Format("02.01.2006"), Locdata.Task, Locdata.State)
					msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, MsgTxt)
					B.CreateButtons(&msg, []map[string]interface{}{
						map[string]interface{}{
							"Alias":    "Отмена мониторинга",
							"ID":       "Cancel",
							"callBack": B.Cancel,
						},
					}, false)
					B.bot.Send(msg)
				}
				if Locdata.End {
					timer.Stop()
					B.Cancel()
				}
			}
		}
	}()
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
	var data = []Data{}

	if err, JSON := fresh.GetListUpdateState(B.date.Format("20060102")); err == nil {
		B.JsonUnmarshal(JSON, &data)
	} else {
		panic(err)
	}

	if len(data) == 0 {
		B.notInvokeInnerFinish = true
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("За дату %v нет данных", B.date.Format("02.01.2006")))

		B.CreateButtons(&msg, []map[string]interface{}{
			map[string]interface{}{
				"Alias":    "Запросить данные за -1 день",
				"ID":       "yes",
				"callBack": B.ChoseYes,
			},
		}, true)

		B.bot.Send(msg)
	} else {
		B.notInvokeInnerFinish = false
		for _, line := range data {
			MsgTxt := fmt.Sprintf("Дата: %v\nЗадание: %v\nСтатус: %q", B.date.Format("02.01.2006"), line.Task, line.State)
			Msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, MsgTxt)
			if !line.End {
				B.notInvokeInnerFinish = true
				callBack := func() {
					B.MonitoringState(line.UUID)
				}
				B.CreateButtons(&Msg, []map[string]interface{}{
					map[string]interface{}{
						"Alias":    "Следить за изменением состояния",
						"ID":       "MonitoringState",
						"callBack": callBack,
					},
				}, true)
			}

			B.bot.Send(Msg)
		}
	}
}

func (B *GetListUpdateState) StartInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.date = time.Now()

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите агент сервиса")
	B.callback = make(map[string]func(), 0)
	Buttons := make([]map[string]interface{}, 0, 0)
	for _, conffresh := range Confs.FreshConf {
		UUID, _ := uuid.NewV4()
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		Buttons = append(Buttons, map[string]interface{}{
			"Alias": conffresh.Alias,
			"ID":    UUID.String(),
			"callBack": func() {
				B.ChoseMC(Name)
			},
		})
	}

	B.CreateButtons(&msg, Buttons, true)
	bot.Send(msg)

	/* B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Загружаем конфигурацию %q в МС", fileName)))

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите менеджер сервиса для загрузки расширений")
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
