package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/sirupsen/logrus"
)

type Bases struct {
	Name string `json:"Name"`
	UUID string `json:"UUID"`
}

type Updates struct {
	Name        string `json:"Name"`
	FromVervion string `json:"FromVervion"`
	ToVervion   string `json:"ToVervion"`
	UUID        string `json:"UUID"`
}

type SetPlanUpdate struct {
	BaseTask

	freshConf   *cf.FreshConf
	UUIDBase    string
	UUIDUpdate  string
	MinuteShift int
	//UpdateUUID string
}

func (B *SetPlanUpdate) ForceUpdate() {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		} else {
			B.innerFinish()
		}
		B.outFinish()
	}()

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	if e := fresh.SetUpdetes(B.UUIDUpdate, B.UUIDBase, B.MinuteShift, true, nil); e != nil {
		panic(e) // в defer перехват
	}

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
	B.UUIDUpdate = ChoseData

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Укажите через сколько минут необходимо запустить обновление.")
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
			msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите число.")
			B.bot.Send(msg)
			result = false
		} else {
			B.MinuteShift = MinuteShift
			fresh := new(fresh.Fresh)
			fresh.Conf = B.freshConf
			if e := fresh.SetUpdetes(B.UUIDUpdate, B.UUIDBase, MinuteShift, false, nil); e != nil {
				result = false
				msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка:\n%v\n\n"+
					"Ошибка может быть из-за того, что есть запланированое и не выполненое задание на обновдение.\n"+
					"Попробовать явно завершить предыдущие задания и обновить повторно?", e.Error()))

				UUID, _ := uuid.NewV4()
				B.CreateButtons(&msg, []map[string]interface{}{
					map[string]interface{}{
						"Caption": "Да",
						"ID":      UUID.String(),
						"Invoke":  B.ForceUpdate,
					}}, 2, true)
				B.bot.Send(msg)
			} else {
				result = true
			}
		}

		return
	}
}

func (B *SetPlanUpdate) AllUpdates() {
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
	if B.UUIDBase == "" {
		panic("Не выбрана база данных")
	}

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetAvailableUpdates(B.UUIDBase, true)
	var updates = []Updates{}

	B.JsonUnmarshal(JSON, &updates)
	B.showUpdates(updates, true)
}

func (B *SetPlanUpdate) showUpdates(updates []Updates, all bool) {
	if len(updates) != 0 {
		TxtMsg := "Выберите обновление:\n"
		Buttons := make([]map[string]interface{}, 0, 0)
		B.callback = make(map[string]func(), 0)

		for _, line := range updates {
			TxtMsg += fmt.Sprintf("\t-%v:\n\t\tОбновляемая версия %q\n\t\tНовая версия %q\n\n", line.Name, line.FromVervion, line.ToVervion)
			UUID, _ := uuid.NewV4()
			locData := line.UUID // Обязательно через переменную, нужно для замыкания
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": line.Name,
				"ID":      UUID.String(),
				"Invoke": func() {
					B.ChoseUpdate(locData)
				},
			})
		}

		if !all {
			UUID, _ := uuid.NewV4()
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": "В списке нет нужного обновления",
				"ID":      UUID.String(),
				"Invoke":  B.AllUpdates,
			})
		}

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, TxtMsg)
		B.CreateButtons(&msg, Buttons, 1, true)
		B.bot.Send(msg)
	} else {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Доступных обновлений не найдено"))
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
	JSON := fresh.GetAvailableUpdates(B.UUIDBase, false)
	var updates = []Updates{}

	B.JsonUnmarshal(JSON, &updates)
	B.showUpdates(updates, false)
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
		Buttons := make([]map[string]interface{}, 0, 0)
		B.callback = make(map[string]func(), 0)

		for _, line := range bases {
			UUID, _ := uuid.NewV4()
			locData := line.UUID // Обязательно через переменную, нужно для замыкания
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": line.Name,
				"ID":      UUID.String(),
				"Invoke": func() {
					B.ChoseBD(locData)
				},
			})
		}

		B.CreateButtons(&msg, Buttons, 1, true)
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
	Buttons := make([]map[string]interface{}, 0, 0)
	B.callback = make(map[string]func(), 0)

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
}

func (B *SetPlanUpdate) innerFinish() {
	B.baseFinishMsg("Готово!")
}
