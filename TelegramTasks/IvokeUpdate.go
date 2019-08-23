package telegram

import (
	JK "1C/jenkins"
	"fmt"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdate struct {
	SetPlanUpdate
}

func (this *IvokeUpdate) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	this.bot = bot
	this.update = update
	this.outFinish = finish
	this.state = StateWork
	this.AppendDescription(this.name)
	this.Start_2()
}

func (this *IvokeUpdate) Start_2() {
	var once sync.Once

	// Инициализируем действия которые нужно сделать после выбора БД
	this.InvokeChoseDB = func(DB *Bases) {
		defer func() {
			if err := recover(); err != nil {
				this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)))
				this.innerFinish()
				this.outFinish()
			}
		}()

		jk := new(JK.Jenkins)
		jk.RootURL = Confs.Jenkins.URL
		jk.User = Confs.Jenkins.Login
		jk.Pass = Confs.Jenkins.Password
		jk.Token = Confs.Jenkins.UserToken
		err := jk.InvokeJob("run_update", map[string]string{
			"srv":      DB.Cluster.MainServer,
			"db":       DB.Name,
			"ras_srv":  DB.Cluster.RASServer,
			"ras_port": fmt.Sprintf("%d", DB.Cluster.RASPort),
			"usr":      DB.UserName,
			"pwd":      DB.UserPass,
		})

		if err == nil {
			// sync.Once нужен на случай когда горутина уже запущена и запустили новое задание, что бы
			// не порождалась еще одна горутина, т.к. смысла в ней нет, pullStatus проверяет статус у всего задания
			once.Do(func() { go this.pullStatus() })
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Задание \"run_update\" "+
				"для базы %q отправлено", DB.Caption)))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, fmt.Sprintf("Произошла ошибка при отправке задания \"run_update\" для базы %q:\n %v", DB.Caption, err)))
		}
	}
	this.appendMany = false
	this.Start() // метод родителя
}

func (this *IvokeUpdate) pullStatus() {
	var once sync.Once
	timeout := time.NewTicker(time.Minute * 5)
	timer := time.NewTicker(time.Second * 10)
	for range timer.C {

		status := JK.GetJobStatus(Confs.Jenkins.URL, "run_update", Confs.Jenkins.Login, Confs.Jenkins.Password)
		switch status {
		case JK.Error:
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Выполнение задания \"run_update\" завершилось с ошибкой"))
			this.innerFinish()
			timer.Stop()
			timeout.Stop()
		case JK.Done:
			this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Задания \"run_update\" выполнено"))
			this.innerFinish()
			timer.Stop()
			timeout.Stop()
		case JK.Undefined:
			// Если у нас статус неопределен, запускаем таймер таймаута, если при запущеном таймере статус поменяется на определенный, мы остановим таймер
			// таймер нужно запустить один раз
			once.Do(func() {
				go func() {
					// используется таймер, а не слип например потому, что должна быть возможность прервать из вне, да можно наверное было бы и через контекст, но зачем так заморачиваться
					<-timeout.C // читаем из канала, нам нужно буквально одного события
					this.bot.Send(tgbotapi.NewMessage(this.GetMessage().Chat.ID, "Задания \"run_update\" не удалось определить статус, прервано по таймауту"))
					this.innerFinish()
					timer.Stop()
					timeout.Stop()
				}()
			})
		}
	}
}

func (this *IvokeUpdate) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.description))
	this.outFinish()
}
