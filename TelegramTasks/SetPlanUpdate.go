package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

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
	//bases       []Bases
	//updates []Updates
	//UpdateUUID string
	InvokeChoseDB func(BD *Bases)
	appendMany    bool
}

func (B *SetPlanUpdate) ForceUpdate(UUIDUpdate, name, UUIDBase string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
		}
		B.invokeEndTask("")
	}()

	B.AppendDescription(fmt.Sprintf("Принудительное обновление %q", name))

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	if e := fresh.SetUpdetes(UUIDUpdate, UUIDBase, B.MinuteShift, true, nil); e != nil {
		panic(e) // в defer перехват
	}

}

func (B *SetPlanUpdate) ChoseUpdate(ChoseData, name, UUIDBase string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.invokeEndTask("")
		}
	}()

	// значит нажали на вторую кнопку, а обновление должно быть выбрано только одно
	/* if B.hookInResponse != nil {
		return
	} */

	if B.freshConf == nil {
		panic("Не определены настройки для МС")
	}
	UUIDUpdate := ChoseData
	B.AppendDescription(fmt.Sprintf("Обновление %q", name))

	msg := tgbotapi.NewMessage(B.ChatID, "Укажите через сколько минут необходимо запустить обновление.")
	B.bot.Send(msg)

	B.hookInResponse = func(update *tgbotapi.Update) (result bool) {
		defer func() {
			if err := recover(); err != nil {
				Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(Msg)
				B.invokeEndTask("")
				result = true
			} else {
				if result {
					B.invokeEndTask("")
				}
			}
		}()

		if MinuteShift, err := strconv.Atoi(B.GetMessage().Text); err != nil {
			msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Введите число. Вы ввели %q", B.GetMessage().Text))
			B.bot.Send(msg)
			result = false
		} else {
			B.MinuteShift = MinuteShift
			fresh := new(fresh.Fresh)
			fresh.Conf = B.freshConf
			if e := fresh.SetUpdetes(UUIDUpdate, UUIDBase, MinuteShift, false, nil); e != nil {
				result = false
				msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Произошла ошибка:\n%v\n\n"+
					"Ошибка может быть из-за того, что есть запланированое и не выполненое задание на обновдение.\n"+
					"Попробовать явно завершить предыдущие задания и обновить повторно?", e.Error()))

				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Да", func() { B.ForceUpdate(UUIDUpdate, name, UUIDBase) })
				B.createButtons(&msg, Buttons, 1, true)
				B.bot.Send(msg)
			} else {
				result = true
				msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("Обновление начнется в %v", time.Now().Add(time.Minute*time.Duration(MinuteShift)).Format("02.01.2006 15.04.05")))
				B.bot.Send(msg)
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
			B.invokeEndTask("")
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

		// Видимо в тележке есть ограничение на вывод текста в сообщении, если запросить все доступные обновления может получиться около 500 строк в сообщении
		// как правило нужно ну 10 последних максимум
		if len(updates) > 10 {
			updates = updates[len(updates)-10:]
		}
		for id, line := range updates {
			TxtMsg += fmt.Sprintf("%v. <b>База:</b>\n %q\n %q:\n<b>Обновляемая версия:</b> %q\n<b>Новая версия:</b> %q\n\n",
				id+1,
				line.NameDB,
				line.Name,
				line.FromVervion,
				line.ToVervion)

			locData := line.UUID // Обязательно через переменную, нужно для замыкания
			name := line.Name
			B.appendButton(&Buttons, fmt.Sprint(id+1), func() { B.ChoseUpdate(locData, name, UUIDBase) })
		}

		if !all {
			B.appendButton(&Buttons, "В списке нет нужного обновления", func() { B.AllUpdates(UUIDBase) })
		}

		msg := tgbotapi.NewMessage(B.ChatID, TxtMsg)
		msg.ParseMode = "HTML"
		B.createButtons(&msg, Buttons, 4, true)
		B.bot.Send(msg)
	} else {
		msg := tgbotapi.NewMessage(B.ChatID, "Доступных обновлений не найдено. Запросить все возможные варианты?")
		Buttons := make([]map[string]interface{}, 0, 0)
		B.appendButton(&Buttons, "Да", func() { B.AllUpdates(UUIDBase) })
		B.createButtons(&msg, Buttons, 4, true)
		B.bot.Send(msg)
	}

}

func (B *SetPlanUpdate) ChoseBD(BD *Bases) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.invokeEndTask("")
		}
	}()

	if B.freshConf == nil {
		logrus.Error("Не определены настройки для МС")
		return
	}

	if B.InvokeChoseDB == nil {
		logrus.Error("Не определено действия с выбранной базой")
		return
	} else {
		B.InvokeChoseDB(BD)
	}
}

func (B *SetPlanUpdate) ChoseManyDB(Bases []*Bases) {
	msg := tgbotapi.NewMessage(B.ChatID, "Введите номера баз через запятую")
	B.bot.Send(msg)

	B.hookInResponse = func(update *tgbotapi.Update) bool {
		defer func() {
			if err := recover(); err != nil {
				Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
				logrus.Error(Msg)
				B.bot.Send(tgbotapi.NewMessage(B.ChatID, Msg))
				B.invokeEndTask("")
			}
		}()

		numbers := strings.Split(B.GetMessage().Text, ",")
		if len(numbers) == 0 {
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Укажите номера через запятую."))
			return false
		}

		//B.bases = make([]string, 0, 0)
		for _, num := range numbers {
			if numInt, err := strconv.Atoi(strings.Trim(num, " ")); err == nil {
				for id, base := range Bases {
					if id+1 == numInt {
						//B.bases = append(B.bases, base.UUID)
						B.ChoseBD(base)
					}
				}
			} else {
				msg := tgbotapi.NewMessage(B.ChatID, "Значение "+num+" не является числом")
				B.bot.Send(msg)
			}
		}
		return true
	}
}

func (this *SetPlanUpdate) ChoseMC(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			logrus.Error(Msg)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, Msg))
			this.invokeEndTask("")
		}
	}()

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			this.freshConf = conffresh
			break
		}
	}

	fresh := new(fresh.Fresh)
	fresh.Conf = this.freshConf

	// например при использовании этого класса из IvokeUpdate нам не нужна кнопка "несколько"
	ChoseManyDB := this.ChoseManyDB
	if !this.appendMany {
		ChoseManyDB = nil
	}

	this.BuildButtonsByBase(fresh.GetDatabase(), new(step), this.ChoseBD, ChoseManyDB)

}

func (this *SetPlanUpdate) BuildButtonsByBase(JSON_Base string, step IStep, ChoseBD func(Bases *Bases), ChoseManyDB func(Bases []*Bases)) (result string) {
	var bases = []*Bases{}
	this.JsonUnmarshal(JSON_Base, &bases)

	// Сортируем
	sort.Slice(bases, func(i, j int) bool {
		b := []string{bases[i].Caption, bases[j].Caption}
		sort.Strings(b)
		return b[0] == bases[i].Caption
	})

	if len(bases) != 0 {
		result = "Выберите базу:\n"

		for id, line := range bases {
			result += fmt.Sprintf("%v. %v - %v\n", id+1, line.Name, line.Caption)

			DB := line // Обязательно через переменную, нужно для замыкания
			step.appendButton(fmt.Sprint(id+1), func() { ChoseBD(DB) })
		}

		if ChoseManyDB != nil {
			step.appendButton("Несколько", func() { ChoseManyDB(bases) })
		}
	} else {
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Баз не найдено"))
	}

	step.reverseButton()

	return result
}

func (this *SetPlanUpdate) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.appendMany = true

	// Инициализируем действия которые нужно сделать после выбоа БД
	this.InvokeChoseDB = func(BD *Bases) {
		this.AppendDescription(fmt.Sprintf("Обновление %q", BD.Caption))

		fresh := new(fresh.Fresh)
		fresh.Conf = this.freshConf
		JSON := fresh.GetAvailableUpdates(BD.UUID, false)
		var updates = []Updates{}

		this.JsonUnmarshal(JSON, &updates)
		this.showUpdates(updates, BD.UUID, false)
	}

	this.AppendDescription(this.name)
	return this
}

func (B *SetPlanUpdate) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	msg := tgbotapi.NewMessage(B.ChatID, "Выберите менеджер сервиса для загрузки конфигурации")
	Buttons := make([]map[string]interface{}, 0, 0)
	B.callback = make(map[string]func(), 0)

	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		B.appendButton(&Buttons, conffresh.Alias, func() { B.ChoseMC(Name) })
	}

	B.createButtons(&msg, Buttons, 3, true)
	B.bot.Send(msg)
}

func (B *SetPlanUpdate) InfoWrapper(task ITask) {
	B.info = "ℹ Команда планирует обновление файла конфигурации через агента сервиса."
	B.BaseTask.InfoWrapper(task)
}
