package telegram

import (
	cf "TelegramBot/Configuration"
	"TelegramBot/fresh"
	"fmt"
	"image/color"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benoitmasson/plotters/piechart"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"

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

	//date                 time.Time
	shiftDate            int
	freshConf            *cf.FreshConf
	notInvokeInnerFinish bool
	timer                map[string]*time.Ticker
	track                map[string]bool
	//data                 map[string]*Data
}

func (B *GetListUpdateState) ChoseAent(ChoseData string) {
	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			B.freshConf = conffresh
			break
		}
	}

	B.getData(0)
}

func (B *GetListUpdateState) DecDate() {
	B.shiftDate--
	B.getData(B.shiftDate)
}

func (B *GetListUpdateState) IncDate() {
	B.shiftDate++
	B.getData(B.shiftDate)
}

func (B *GetListUpdateState) Cancel(key string) {
	B.notInvokeInnerFinish = false
	B.track[key] = false

	// на случай если кто-то 2 раза на кнопку нажмет
	if t, ok := B.timer[key]; ok {
		t.Stop()
		B.bot.Send(tgbotapi.NewMessage(B.ChatID, "Мониторинг отменен"))
		delete(B.timer, key)
	}

	if len(B.timer) == 0 {
		B.innerFinish()
	}

}

func (B *GetListUpdateState) MonitoringState(UUIDs []string, key string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при получении состояние задания: %v", err)
			logrus.Error(Msg)
		}
	}()

	if _, ok := B.timer[key]; ok {
		return // значит уже отслеживается
	}

	// B.AppendDescription(fmt.Sprintf("Мониторинг за %q", name))

	Msg := tgbotapi.NewMessage(B.ChatID, "При изменении данных задания будет уведомление")
	B.bot.Send(Msg)

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	var buffHash = map[string]string{}

	B.timer[key] = time.NewTicker(time.Minute)
	B.track[key] = true

	//ctx, finish := context.WithCancel(context.Background())
	go func() {
		var Locdata = new(Data)

		for range B.timer[key].C {
			allTaskEnd := true
			showMsg := false
			MsgTxt := fmt.Sprintf("<b>Дата:</b> %v\n\n", time.Now().AddDate(0, 0, B.shiftDate).Format("02.01.2006"))
			for _, UUID := range UUIDs {
				if JSON, err := fresh.GeUpdateState(UUID); err == nil {
					B.JsonUnmarshal(JSON, Locdata)
					if hash := Locdata.Hash(); buffHash[UUID] != hash {
						MsgTxt += fmt.Sprintf("<b>Задание:</b> %q\n<b>Статус:</b> %q\n<b>Последние действие:</b> %q\n\n", Locdata.Task, Locdata.State, Locdata.LastAction)
						showMsg = true
						buffHash[UUID] = hash
					}
					allTaskEnd = allTaskEnd && Locdata.End
				}
			}
			if showMsg {
				//fmt.Println(MsgTxt)
				msg := tgbotapi.NewMessage(B.ChatID, MsgTxt)
				msg.ParseMode = "HTML"
				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Отмена мониторинга", func() { B.Cancel(key) })
				B.createButtons(&msg, Buttons, 3, false)
				B.bot.Send(msg)
			}

			if allTaskEnd {
				B.Cancel(key)
			}

			/* select {
			case <-ctx.Done():
				timer.Stop()
			} */
		}
	}()
}

func (B *GetListUpdateState) getData(shiftDate int) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", B.name, err)
			logrus.Error(Msg)
			B.bot.Send(tgbotapi.NewMessage(B.ChatID, Msg))
			B.invokeEndTask("")
		}
	}()

	fresh := new(fresh.Fresh)
	fresh.Conf = B.freshConf
	var data = []Data{}

	if JSON, err := fresh.GetListUpdateState(shiftDate); err == nil {
		B.JsonUnmarshal(JSON, &data)
	} else {
		panic(err)
	}

	// notInvokeInnerFinish нужен что бы регулировать окончанием задания
	if len(data) == 0 {
		B.goTo(1, fmt.Sprintf("За дату %v нет данных", time.Now().AddDate(0, 0, B.shiftDate).Format("02.01.2006")))
		B.notInvokeInnerFinish = true
		return
	}

	B.notInvokeInnerFinish = false
	groupState, _ := B.dataUniq(data)
	for key, line := range groupState {
		createButton := false
		tasks := []string{}
		for i, _ := range line {
			createButton = createButton || !line[i].End
			tasks = append(tasks, fmt.Sprintf("%d) %v", i+1, line[i].Task))
		}

		MsgTxt := fmt.Sprintf("<b>%v:</b>\n<pre>%v</pre>", key, strings.Join(tasks, "\n----------\n"))
		msg := tgbotapi.NewMessage(B.ChatID, MsgTxt)
		msg.ParseMode = "HTML"

		if createButton {
			UUIDs := []string{}
			for _, l := range line {
				UUIDs = append(UUIDs, l.UUID)
			}

			_key := key // для замыкания
			if !B.track[_key] {
				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Следить за изменением состояния", func() {
					B.notInvokeInnerFinish = true
					B.MonitoringState(UUIDs, _key)
				})
				B.createButtons(&msg, Buttons, 1, false)
			} else {
				Buttons := make([]map[string]interface{}, 0, 0)
				B.appendButton(&Buttons, "Отменить слежение", func() {
					B.notInvokeInnerFinish = true
					B.Cancel(_key)
				})
				B.createButtons(&msg, Buttons, 1, false)
			}
		}

		B.bot.Send(msg)
	}

	///////////////// Cart  ////////////////////
	filepath := B.buildhart(data)
	if _, err := os.Stat(filepath); !os.IsNotExist(err) {
		msg := tgbotapi.NewPhotoUpload(B.ChatID, filepath)
		B.bot.Send(msg)

		os.Remove(filepath)
	}
	//////////////////////////////////////////

	// Хотел сделать выбор график или список
	// msg := tgbotapi.NewMessage(B.ChatID, "Как подать информацию?")
	// Buttons := make([]map[string]interface{}, 0, 0)
	// B.appendButton(&Buttons, "Списком", func() { B.showlist(data) })
	// B.appendButton(&Buttons, "Графиком", func() { B.showchart(data) })
	// B.createButtons(&msg, Buttons, 3, true)
	// B.bot.Send(msg)
}

func (B *GetListUpdateState) dataUniq(data []Data) (map[string][]Data, int) {
	groupState := make(map[string][]Data, 0)

	// в data могут быть нескольео ошибок по одному и тому же заданию, наприемр если его запускали несколько раз и оно несколько раз падало.
	// по этому удаляем из data дубли по Task + State
	// так же может быть случай когда один раз упало задание в агента с ошибкой, а второй раз выполнилось, т.е. приоритет должен быть у статусов, брать максимальный статус
	// приоритеты такие:
	states := map[string]int{
		"Выполняется":          6,
		"Завершено":            5,
		"Ожидает выполнения":   4,
		"Завершено с ошибками": 3,
		"Остановлено":          2,
		"Отменено":             1,
	}

	priorityItems := make(map[string]struct {
		priority int
		line     Data
	}, 0)
	for _, line := range data {
		if priority, ok := states[line.State]; !ok || priority > priorityItems[line.Task].priority {
			priorityItems[line.Task] = struct {
				priority int
				line     Data
			}{
				priority: states[line.State],
				line:     line,
			}

		}
	}

	// Перегруппировываем по статусу
	total := 0
	for _, v := range priorityItems {
		groupState[v.line.State] = append(groupState[v.line.State], v.line)
		total++
	}

	return groupState, total
}

func (B *GetListUpdateState) buildhart(data []Data) string {
	rand.Seed(time.Now().UnixNano())

	p, err := plot.New()
	if err != nil {
		panic(err)
	}
	p.Legend.Top = true
	p.HideAxes()

	tmpDir, err := ioutil.TempDir("", "")
	if err != nil {
		return ""
	}
	chartFile := filepath.Join(tmpDir, "chart.png")

	offset := 0
	groupState, total := B.dataUniq(data)
	for state, line := range groupState {
		pie, err := piechart.NewPieChart(plotter.Values{float64(len(line))})
		if err != nil {
			panic(err)
		}
		pie.Color = color.RGBA{uint8(rand.Intn(255)), uint8(rand.Intn(255)), uint8(rand.Intn(255)), 255}
		pie.Offset.Value = float64(offset)
		pie.Total = float64(total)
		pie.Labels.Nominal = []string{state}
		pie.Labels.Values.Show = true
		//pie.Labels.Values.Percentage = true // что бы выводилось в %

		p.Add(pie)
		p.Legend.Add(state, pie)

		offset += len(line)
	}

	if err := p.Save(500, 500, chartFile); err != nil {
		return ""
	} else {
		return chartFile
	}

}

func (B *GetListUpdateState) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)

	if B.timer == nil {
		B.timer = make(map[string]*time.Ticker, 0)
	}
	if B.track == nil {
		B.track = make(map[string]bool, 0)
	}

	firstStep := new(step).Construct("Выберите агент сервиса", "Шаг1", B, ButtonCancel, 2)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(conffresh.Alias, func() { B.ChoseAent(Name) })
	}

	B.steps = []IStep{
		firstStep,
		new(step).Construct("", "Шаг2", B, ButtonCancel|ButtonBack, 1).
			appendButton("Запросить данные за -1 день", B.DecDate).
			appendButton("Запросить данные за +1 день", B.IncDate),
	}

	return B
}

func (B *GetListUpdateState) Start() {
	logrus.WithField("description", B.GetDescription()).Debug("Start")

	B.steps[B.currentStep].invoke(&B.BaseTask)
}

func (B *GetListUpdateState) innerFinish() {
	if B.notInvokeInnerFinish {
		return
	}

	B.invokeEndTask("")
}

func (B *GetListUpdateState) InfoWrapper(task ITask) {
	B.info = "ℹ Команда получает список запланированных за сегодня обновлений в агенте сервиса."
	B.BaseTask.InfoWrapper(task)
}
