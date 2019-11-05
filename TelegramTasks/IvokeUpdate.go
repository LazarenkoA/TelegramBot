package telegram

import (
	JK "1C/jenkins"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdate struct {
	SetPlanUpdate
}

func (this *IvokeUpdate) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.AppendDescription(this.name)

	return this
}

func (this *IvokeUpdate) Start() {
	var once sync.Once

	logrus.WithField("description", this.GetDescription()).Debug("Start")

	// Инициализируем действия которые нужно сделать после выбора БД
	this.InvokeChoseDB = func(DB *Bases) {
		defer func() {
			if err := recover(); err != nil {
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)))
				this.innerFinish()
				this.outFinish()
			}
		}()

		jk := new(JK.Jenkins).Create("run_update")
		jk.RootURL = Confs.Jenkins.URL
		jk.User = Confs.Jenkins.Login
		jk.Pass = Confs.Jenkins.Password
		jk.Token = Confs.Jenkins.UserToken
		err := jk.InvokeJob(map[string]string{
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

			once.Do(func() {
				go jk.CheckStatus(
					func() {
						this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"run_update\" выполнено успешно."))
						this.innerFinish()
					},
					func() {
						this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Выполнение задания \"run_update\" завершилось с ошибкой"))
						this.innerFinish()
					},
					func() {
						this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"run_update\" не удалось определить статус, прервано по таймауту"))
						this.innerFinish()
					},
				)
			})
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Задание \"run_update\" "+
				"для базы %q отправлено", DB.Caption)))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при отправке задания \"run_update\" для базы %q:\n %v", DB.Caption, err)))
		}
	}
	this.appendMany = false
	this.SetPlanUpdate.Start() // метод родителя
}

func (this *IvokeUpdate) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

func (B *IvokeUpdate) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда запускает админский сеанс у выбранных баз с параметром ЗапуститьОбновлениеИнформационнойБазы (через jenkins). " +
		"Задание в jenkins run_update."
	B.BaseTask.InfoWrapper(task)
}
