package telegram

import (
	cf "1C/Configuration"
	"1C/fresh"
	"fmt"
	"image/color"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
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

	if JSON, err := fresh.GeUpdateState(UUID); err == nil {
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
			if JSON, err := fresh.GeUpdateState(UUID); err == nil {
				B.JsonUnmarshal(JSON, Locdata)
				if Locdata.Hash() != data.Hash() {
					*data = *Locdata // обновляем данные, не ссылку, это важно

					MsgTxt := fmt.Sprintf("Дата: %v\n<b>Задание:</b> %q\n<b>Статус:</b> %q\n<b>Последние действие:</b> %q", time.Now().AddDate(0, 0, B.shiftDate).Format("02.01.2006"), Locdata.Task, Locdata.State, Locdata.LastAction)
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

func (B *GetListUpdateState) getData(shiftDate int) {
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

		MsgTxt := fmt.Sprintf("<b>Дата:</b> %v\n<b>Задание:</b> %v\n<b>Статус:</b> %v", time.Now().AddDate(0, 0, B.shiftDate).Format("02.01.2006"), line.name, line.state)
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

func (B *GetListUpdateState) buildhart(data []Data) string {
	groupState := make(map[string]int, 0)
	rand.Seed(time.Now().UnixNano())

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
		"undef":                0,
	}
	priority := map[int]string{
		6: "Выполняется",
		5: "Завершено",
		4: "Ожидает выполнения",
		3: "Завершено с ошибками",
		2: "Остановлено",
		1: "Отменено",
		0: "undef",
	}

	uniqueItems := make(map[string]int, 0)
	for _, line := range data {
		StatePriority := states[line.State]
		uniqueItems[line.Task] = int(math.Max(float64(uniqueItems[line.Task]), float64(StatePriority)))
	}

	total := 0
	for _, value := range uniqueItems {
		groupState[priority[value]]++
		total++
	}

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
	for state, count := range groupState {
		pie, err := piechart.NewPieChart(plotter.Values{float64(count)})
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

		offset += count
	}

	if err := p.Save(500, 500, chartFile); err != nil {
		return ""
	} else {
		return chartFile
	}

}

func (B *GetListUpdateState) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	B.BaseTask.Initialise(bot, update, finish)

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

	B.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", B.GetDescription()))
}

func (B *GetListUpdateState) InfoWrapper(task ITask) {
	B.info = "ℹ Команда получает список запланированных за сегодня обновлений в агенте сервиса."
	B.BaseTask.InfoWrapper(task)
}
