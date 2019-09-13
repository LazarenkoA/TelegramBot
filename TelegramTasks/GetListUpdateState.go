package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
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
	track                map[string]bool
	//data                 map[string]*Data
}

func (B *GetListUpdateState) ChoseMC(ChoseData string) {
	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	// Первый запрос без даты т.к. агент отдаст за сегодня, но сегодня я передать не могу т.к. не красиво получается
	// из-за часовых поясов, я в 22:30 запрашиваю данные и не вижу их
	B.getData("")
}

func (B *GetListUpdateState) ChoseYes() {
	B.date = B.date.AddDate(0, 0, -1)
	B.getData(B.date.Format("20060102"))
}

func (B *GetListUpdateState) Cancel(UUID string) {
	B.notInvokeInnerFinish = false
	B.track[UUID] = false

	// на случай если кто-то 2 раза на кнопку нажмет
	if t, ok := B.timer[UUID]; ok {
		t.Stop()
		B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Мониторинг отменен"))
		delete(B.timer, UUID)
	}

	if len(B.timer) == 0 {
		B.innerFinish()
		B.outFinish()
	}

}

func (B *GetListUpdateState) MonitoringState(UUID, name string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при получении состояние задания: %v", err)
			logrus.Error(Msg)
		}
	}()

	if _, ok := B.timer[UUID]; ok {
		return // значит уже отслеживается
	}

	B.AppendDescription(fmt.Sprintf("Мониторинг за %q", name))

	Msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("При изменении данных задания %q будет уведомление", name))
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
	B.track[UUID] = true

	//ctx, finish := context.WithCancel(context.Background())
	go func() {
		var Locdata = new(Data)

		for range B.timer[UUID].C {
			if err, JSON := fresh.GeUpdateState(UUID); err == nil {
				B.JsonUnmarshal(JSON, Locdata)
				if Locdata.Hash() != data.Hash() {
					*data = *Locdata // обновляем данные, не ссылку, это важно

					MsgTxt := fmt.Sprintf("Дата: %v\n<b>Задание:</b> %q\n<b>Статус:</b> %q\n<b>Последние действие:</b> %q", B.date.Format("02.01.2006"), Locdata.Task, Locdata.State, Locdata.LastAction)
					msg := tgbotapi.NewMessage(B.ChatID, MsgTxt)
					msg.ParseMode = "HTML"

					Buttons := make([]map[string]interface{}, 0, 0)
					B.appendButton(&Buttons, "Отмена мониторинга", func() { B.Cancel(UUID) })
					B.createButtons(&msg, Buttons, 3, false)
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

func (B *GetListUpdateState) getData(date string) {
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

	if err, JSON := fresh.GetListUpdateState(date); err == nil {
		B.JsonUnmarshal(JSON, &data)
	} else {
		panic(err)
	}

	if len(data) == 0 {
		B.notInvokeInnerFinish = true
		msg := tgbotapi.NewMessage(B.ChatID, fmt.Sprintf("За дату %v нет данных", B.date.Format("02.01.2006")))

		Buttons := make([]map[string]interface{}, 0, 0)
		B.appendButton(&Buttons, "Запросить данные за -1 день", B.ChoseYes)
		B.createButtons(&msg, Buttons, 1, true)

		B.bot.Send(msg)
	} else {
		B.notInvokeInnerFinish = false
		groupTask := make(map[bool][]struct {
			UUID  string
			name  string
			state string
		}, 0)

		// Группируем задания, что бы все завершенные выводились в одном списке, а активные по отдельности (что бы можно было подписаться на изменения)
		for _, line := range data {
			groupTask[line.End] = append(groupTask[line.End], struct {
				UUID  string
				name  string
				state string
			}{line.UUID, line.Task, line.State})
		}

		// Выводим завершенные
		groupState := make(map[string][]string, 0)

		// Сортируем по статусу, сортировка нужна для нумерации списка
		sort.Slice(groupTask[true], func(i, j int) bool {
			b := []string{groupTask[true][i].state, groupTask[true][j].state}
			sort.Strings(b)
			return b[0] == groupTask[true][i].state
		})

		i := 0
		for _, line := range groupTask[true] {
			// завершенные группируем по статусу
			if _, ok := groupState[line.state]; !ok {
				i = 0
			}

			i++
			groupState[line.state] = append(groupState[line.state], fmt.Sprintf("%d) %v\n---", i, line.name))

		}

		for state, tasks := range groupState {
			MsgTxt := fmt.Sprintf("<b>%v:</b>\n<pre>%v</pre>", state, strings.Join(tasks, "\n"))
			msg := tgbotapi.NewMessage(B.ChatID, MsgTxt)
			msg.ParseMode = "HTML"
			B.bot.Send(msg)
		}

		// Выводим не завершенные
		for _, line := range groupTask[false] {
			UUID := line.UUID // для замыкания
			name := line.name

			MsgTxt := fmt.Sprintf("<b>Дата:</b> %v\n<b>Задание:</b> %v\n<b>Статус:</b> %v", B.date.Format("02.01.2006"), line.name, line.state)
			msg := tgbotapi.NewMessage(B.ChatID, MsgTxt)
			msg.ParseMode = "HTML"

			if B.track == nil {
				B.track = make(map[string]bool, 0)
			}
			if B.timer == nil {
				B.timer = make(map[string]*time.Ticker, 0)
			}

			B.notInvokeInnerFinish = true
			if !B.track[UUID] {
				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Следить за изменением состояния", func() { B.MonitoringState(UUID, name) })
				B.createButtons(&msg, Buttons, 1, false)
			} else {
				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Отменить слежение", func() { B.Cancel(UUID) })
				B.createButtons(&msg, Buttons, 1, false)
			}

			B.bot.Send(msg)
		}

	}
}

func (B *GetListUpdateState) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)
	B.date = time.Now()
	B.AppendDescription(B.name)

	return B
}

func (B *GetListUpdateState) Start() {
	msg := tgbotapi.NewMessage(B.ChatID, "Выберите агент сервиса")
	B.callback = make(map[string]func(), 0)
	Buttons := make([]map[string]interface{}, 0, 0)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		B.appendButton(&Buttons, conffresh.Alias, func() { B.ChoseMC(Name) })
	}

	B.createButtons(&msg, Buttons, 3, true)
	B.bot.Send(msg)
}

func (B *GetListUpdateState) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}

	B.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", B.GetDescription()))
}

func (B *GetListUpdateState) InfoWrapper(task ITask) {
	B.info = "ℹ Команда получает список запланированных за сегодня обновлений в агенте сервиса."
	B.BaseTask.InfoWrapper(task)
}
