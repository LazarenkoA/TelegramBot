package telegram

import (
	JK "1C/jenkins"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdate struct {
	SetPlanUpdate

	pulling bool
	//freshConf *cf.FreshConf
}

func (this *IvokeUpdate) Ini(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	this.bot = bot
	this.update = update
	this.outFinish = finish
	this.state = StateWork
	this.AppendDescription(this.name)
	this.startInitialiseDesc(bot, update, finish)
}

func (this *IvokeUpdate) startInitialiseDesc(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {

	// Инициализируем действия которые нужно сделать после выбоа БД
	this.InvokeChoseDB = func(DB *Bases) {
		defer func() {
			if err := recover(); err != nil {
				bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)))
				this.innerFinish()
				this.outFinish()
			}
		}()

		jk := new(JK.Jenkins)
		jk.RootURL = Confs.Jenkins.URL
		err := jk.InvokeJob("run_update", map[string]string{
			"srv":      DB.Cluster.MainServer,
			"db":       DB.Name,
			"ras_srv":  DB.Cluster.RASServer,
			"ras_port": fmt.Sprintf("%d", DB.Cluster.RASPort),
			"usr":      DB.UserName,
			"pwd":      DB.UserPass,
		})

		if err == nil {
			// pulling нужен на случай когда горутина уже запущена и выбрализапустили новое задание
			// не порождалась еще одна горутина, т.к. смысла в ней нет, pullStatus проверяет статус у всего задания
			if !this.pulling {
				go this.pullStatus()
			}
			bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Задание \"run_update\" "+
				"для базы %q отправлено", DB.Caption)))
		} else {
			bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка при отправке задания \"run_update\" для базы %q:\n %v", DB.Caption, err)))
		}
	}
	this.appendMany = false
	this.startInitialise(bot, update, finish)
}

func (this *IvokeUpdate) pullStatus() {
	defer func() { this.pulling = false }()
	this.pulling = true

	timer := time.NewTicker(time.Second * 10)
	for range timer.C {
		status := JK.GetJobStatus(Confs.Jenkins.URL, "run_update", "", "")
		switch status {
		case JK.Error:
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Выполнение задания \"run_update\" завершилось с ошибкой"))
			this.innerFinish()
			timer.Stop()
		case JK.Done:
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Задания \"run_update\" выполнено"))
			this.innerFinish()
			timer.Stop()
		}
	}
}

func (this *IvokeUpdate) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.description))
	this.outFinish()
}
