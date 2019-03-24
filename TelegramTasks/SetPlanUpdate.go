package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	NameDB      string `json:"NameDB"`
}

type SetPlanUpdate struct {
	BaseTask

	freshConf *cf.FreshConf
	//UUIDBase    string
	//UUIDUpdate  string
	MinuteShift int
	//bases       []string
	//UpdateUUID string
}

func (B *SetPlanUpdate) ForceUpdate(UUIDUpdate, UUIDBase string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		} else {
			//		B.innerFinish()
		}
		//	B.outFinish()
	}()

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	if e := fresh.SetUpdetes(UUIDUpdate, UUIDBase, B.MinuteShift, true, nil); e != nil {
		panic(e) // в defer перехват
	}

}

func (B *SetPlanUpdate) ChoseUpdate(ChoseData, UUIDBase string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.baseFinishMsg(Msg)
		}
	}()

	// значит нажали на вторую кнопку, а обновление должно быть выбрано только одно
	if B.hookInResponse != nil {
		return
	}

	if B.freshConf == nil {
		panic("Не определены настройки для МС")
	}
	UUIDUpdate := ChoseData

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
					B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Готово."))
					// мешает когда несколько баз обновляется
					//B.innerFinish()
					//B.outFinish()
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
			if e := fresh.SetUpdetes(UUIDUpdate, UUIDBase, MinuteShift, false, nil); e != nil {
				result = false
				msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка:\n%v\n\n"+
					"Ошибка может быть из-за того, что есть запланированое и не выполненое задание на обновдение.\n"+
					"Попробовать явно завершить предыдущие задания и обновить повторно?", e.Error()))

				UUID, _ := uuid.NewV4()
				B.CreateButtons(&msg, []map[string]interface{}{
					map[string]interface{}{
						"Caption": "Да",
						"ID":      UUID.String(),
						"Invoke":  func() { B.ForceUpdate(UUIDUpdate, UUIDBase) },
					}}, 2, true)
				B.bot.Send(msg)
			} else {
				result = true
			}
		}

		return
	}
}

func (B *SetPlanUpdate) AllUpdates(UUIDBase string) {
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
	if UUIDBase == "" {
		panic("Не выбрана база данных")
	}

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetAvailableUpdates(UUIDBase, true)
	var updates = []Updates{}

	B.JsonUnmarshal(JSON, &updates)
	B.showUpdates(updates, UUIDBase, true)
}

func (B *SetPlanUpdate) showUpdates(updates []Updates, UUIDBase string, all bool) {

	if len(updates) != 0 {
		Buttons := make([]map[string]interface{}, 0, 0)
		TxtMsg := "Выберите обновление:\n"
		//B.callback = make(map[string]func(), 0)

		for id, line := range updates {
			TxtMsg += fmt.Sprintf("%v. База:\n\t %q\n %q:\n\t\tОбновляемая версия %q\n\t\tНовая версия %q\n\n",
				id+1,
				line.NameDB,
				line.Name,
				line.FromVervion,
				line.ToVervion)

			UUID, _ := uuid.NewV4()
			locData := line.UUID // Обязательно через переменную, нужно для замыкания
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": fmt.Sprint(id + 1),
				"ID":      UUID.String(),
				"Invoke": func() {
					B.ChoseUpdate(locData, UUIDBase)
				},
			})
		}

		if !all {
			UUID, _ := uuid.NewV4()
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": "В списке нет нужного обновления",
				"ID":      UUID.String(),
				"Invoke":  func() { B.AllUpdates(UUIDBase) },
			})
		}

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, TxtMsg)
		B.CreateButtons(&msg, Buttons, 4, true)
		B.bot.Send(msg)
	} else {
		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Доступных обновлений не найдено. Запросить все возможные варианты?")
		UUID, _ := uuid.NewV4()
		Buttons := []map[string]interface{}{
			map[string]interface{}{
				"Caption": "Да",
				"ID":      UUID.String(),
				"Invoke":  func() { B.AllUpdates(UUIDBase) },
			}}
		B.CreateButtons(&msg, Buttons, 4, true)
		B.bot.Send(msg)
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

	UUIDBase := ChoseData

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	JSON := fresh.GetAvailableUpdates(UUIDBase, false)
	var updates = []Updates{}

	B.JsonUnmarshal(JSON, &updates)
	B.showUpdates(updates, UUIDBase, false)
}

func (B *SetPlanUpdate) ChoseManyDB(Bases *[]Bases) {
	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Введите номера баз через запятую")
	B.bot.Send(msg)

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		defer func() {
			if err := recover(); err != nil {
				Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(Msg)
				B.baseFinishMsg(Msg)
			}
		}()

		numbers := strings.Split(B.GetMessage().Text, ",")
		if len(numbers) == 0 {
			B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Укажите номера через запятую."))
			return false
		}

		//B.bases = make([]string, 0, 0)
		for _, num := range numbers {
			if numInt, err := strconv.Atoi(strings.Trim(num, " ")); err == nil {
				for id, base := range *Bases {
					if id+1 == numInt {
						//B.bases = append(B.bases, base.UUID)
						B.ChoseBD(base.UUID)
					}
				}
			} else {
				msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Значение "+num+" не является числом")
				B.bot.Send(msg)
			}
		}
		return true
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

	sort.Slice(bases, func(i, j int) bool {
		b := []string{bases[i].Name, bases[j].Name}
		sort.Strings(b)
		return b[0] == bases[i].Name
	})

	if len(bases) != 0 {
		Buttons := make([]map[string]interface{}, 0, 0)
		//B.callback = make(map[string]func(), 0)
		msgTxt := "Выберите базу:\n"

		for id, line := range bases {
			msgTxt += fmt.Sprintf("%v.  %v\n", id+1, line.Name)

			UUID, _ := uuid.NewV4()
			locData := line.UUID // Обязательно через переменную, нужно для замыкания
			Buttons = append(Buttons, map[string]interface{}{
				"Caption": fmt.Sprint(id + 1),
				"ID":      UUID.String(),
				"Invoke": func() {
					B.ChoseBD(locData)
				},
			})
		}

		UUID, _ := uuid.NewV4()
		Buttons = append(Buttons, map[string]interface{}{
			"Caption": "Несколько",
			"ID":      UUID.String(),
			"Invoke":  func() { B.ChoseManyDB(&bases) },
		})

		msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, msgTxt)
		B.CreateButtons(&msg, Buttons, 4, true)
		B.bot.Send(msg)
	} else {
		B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Баз не найдено"))
	}

}

func (B *SetPlanUpdate) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.state = StateWork
	B.AppendDescription(B.name)
	B.startInitialise(bot, update, finish)
}

func (B *SetPlanUpdate) startInitialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
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
	B.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", B.description))
}
