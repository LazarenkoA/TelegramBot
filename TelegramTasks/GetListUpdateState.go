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
	Task       string `json:"Task"`
	Error      bool   `json:"Error"`
	State      string `json:"State"`
	UUID       string `json:"UUID"`
	LastAction string `json:"LastAction"`
	End        bool   `json:"End"`
}

func (d *Data) Hash() string {
	return GetHash(fmt.Sprintf("%v %v %v %v %v %v", d.Task, d.UUID, d.State, d.Error, d.End, d.LastAction))
}

type GetListUpdateState struct {
	BaseTask

	date                 time.Time
	freshConf            *cf.FreshConf
	notInvokeInnerFinish bool
	timer                map[string]*time.Ticker
	follow               map[string]bool
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

func (B *GetListUpdateState) Cancel(UUID string) {
	B.notInvokeInnerFinish = false
	B.follow[UUID] = false

	// на случай если кто-то 2 раза на кнопку нажет
	if t, ok := B.timer[UUID]; ok {
		t.Stop()
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Мониторинг отменен"))
		delete(B.timer, UUID)
	}

	if len(B.timer) == 0 {
		B.innerFinish()
		B.outFinish()
	}

}

func (B *GetListUpdateState) MonitoringState(UUID string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при получении состояние задания: %v", err)
			logrus.Error(Msg)
		}
	}()

	if _, ok := B.timer[UUID]; ok {
		return // значит уже отслеживается
	}

	Msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "При изменении состояния будет уведомление")
	B.bot.Send(Msg)

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	var data = new(Data)

	if err, JSON := fresh.GeUpdateState(UUID); err == nil {
		B.JsonUnmarshal(JSON, data)
	} else {
		panic(err)
	}

	B.timer[UUID] = time.NewTicker(time.Minute)
	B.follow[UUID] = true

	//ctx, finish := context.WithCancel(context.Background())
	go func() {
		var Locdata = new(Data)

		for range B.timer[UUID].C {
			if err, JSON := fresh.GeUpdateState(UUID); err == nil {
				B.JsonUnmarshal(JSON, Locdata)
				if Locdata.Hash() != data.Hash() {
					data = Locdata

					MsgTxt := fmt.Sprintf("Дата: %v\nЗадание: %q\nСтатус: %q\nПоследние действие: %q", B.date.Format("02.01.2006"), Locdata.Task, Locdata.State, Locdata.LastAction)
					msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, MsgTxt)
					U, _ := uuid.NewV4()
					B.CreateButtons(&msg, []map[string]interface{}{
						map[string]interface{}{
							"Caption": "Отмена мониторинга",
							"ID":      U.String(),
							"Invoke":  func() { B.Cancel(UUID) },
						},
					}, 3, false)
					B.bot.Send(msg)
				}
				if Locdata.End {
					B.Cancel(UUID)
				}
			}

			/* select {
			case <-ctx.Done():
				timer.Stop()
			} */
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
				"Caption": "Запросить данные за -1 день",
				"ID":      "yes",
				"Invoke":  B.ChoseYes,
			},
		}, 1, true)

		B.bot.Send(msg)
	} else {
		B.notInvokeInnerFinish = false
		for _, line := range data {
			UUID := line.UUID // для замыкания

			MsgTxt := fmt.Sprintf("Дата: %v\nЗадание: %q\nСтатус: %q", B.date.Format("02.01.2006"), line.Task, line.State)
			Msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, MsgTxt)
			if !line.End {
				if B.follow == nil {
					B.follow = make(map[string]bool, 0)
				}
				if B.timer == nil {
					B.timer = make(map[string]*time.Ticker, 0)
				}

				B.notInvokeInnerFinish = true
				if !B.follow[UUID] {
					U, _ := uuid.NewV4()
					B.CreateButtons(&Msg, []map[string]interface{}{
						map[string]interface{}{
							"Caption": "Следить за изменением состояния",
							"ID":      U.String(),
							"Invoke": func() {
								B.MonitoringState(UUID)
							},
						},
					}, 1, false)
				} else {
					U, _ := uuid.NewV4()
					B.CreateButtons(&Msg, []map[string]interface{}{
						map[string]interface{}{
							"Caption": "Отменить слежение",
							"ID":      U.String(),
							"Invoke":  func() { B.Cancel(UUID) },
						},
					}, 1, false)
				}
			}

			B.bot.Send(Msg)
		}
	}
}

func (B *GetListUpdateState) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.date = time.Now()
	B.AppendDescription(B.name)

	B.startInitialise(bot, update, finish)
}

func (B *GetListUpdateState) startInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Выберите агент сервиса")
	B.callback = make(map[string]func(), 0)
	Buttons := make([]map[string]interface{}, 0, 0)
	for _, conffresh := range Confs.FreshConf {
		UUID, _ := uuid.NewV4()
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		Buttons = append(Buttons, map[string]interface{}{
			"Caption": conffresh.Alias,
			"ID":      UUID.String(),
			"Invoke": func() {
				B.ChoseMC(Name)
			},
		})
	}

	B.CreateButtons(&msg, Buttons, 3, true)
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

	B.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", B.description))
}
