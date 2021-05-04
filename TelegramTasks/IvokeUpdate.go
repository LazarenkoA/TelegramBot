package telegram

import (
	"fmt"
	JK "github.com/LazarenkoA/TelegramBot/Jenkins"
	"github.com/sirupsen/logrus"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdate struct {
	SetPlanUpdate
}

func (this *IvokeUpdate) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.SetPlanUpdate.Initialise(bot, update, finish)

	this.AppendDescription(this.name)

	return this
}

func (this *IvokeUpdate) Start() {
	//var once sync.Once

	logrus.WithField("description", this.GetDescription()).Debug("Start")

	// Инициализируем действия которые нужно сделать после выбора БД
	this.InvokeChoseDB = func(DB *Bases) {
		defer func() {
			if err := recover(); err != nil {
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)))
				this.invokeEndTask("")
			}
		}()

		jk := new(JK.Jenkins).Create("run_update")
		jk.RootURL = Confs.Jenkins.URL
		jk.User = Confs.Jenkins.Login
		jk.Pass = Confs.Jenkins.Password
		jk.Token = Confs.Jenkins.UserToken
		err := jk.InvokeJob(map[string]string{
			"srv":       DB.Cluster.MainServer,
			"db":        DB.Name,
			"ras_srv":   DB.Cluster.RASServer,
			"ras_port":  fmt.Sprintf("%d", DB.Cluster.RASPort),
			"usr":       strings.Trim(DB.UserName, " "),
			"pwd":       DB.UserPass,
			"jobID":     jk.JobID,
			"v8version": DB.PlatformVersion,
		})

		if err == nil {
			// sync.Once нужен на случай когда горутина уже запущена и запустили новое задание, что бы
			// не порождалась еще одна горутина, т.к. смысла в ней нет, pullStatus проверяет статус у всего задания

			//once.Do(func() {
			go jk.CheckStatus(
				func() {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"run_update\" выполнено успешно."))
					this.invokeEndTask("")
				},
				func() {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задания \"run_update\" не удалось определить статус, прервано по таймауту"))
					this.invokeEndTask("")
				},
				func(err string) {
					msg := tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Выполнение задания \"<b>run_update</b>\" завершилось с ошибкой:\n<pre>%v</pre>", err))
					msg.ParseMode = "HTML"
					this.bot.Send(msg)
					this.invokeEndTask("")
				},
			)
			//})

			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Задание \"run_update\" "+
				"для базы %q отправлено", DB.Caption)))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при отправке задания \"run_update\" для базы %q:\n %v", DB.Caption, err)))
		}
	}
	//this.appendMany = false
	this.CurrentStep().invoke(&this.BaseTask)
}

func (B *IvokeUpdate) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда запускает админский сеанс у выбранных баз с параметром ЗапуститьОбновлениеИнформационнойБазы (через jenkins). " +
		"Задание в jenkins run_update."
	B.BaseTask.InfoWrapper(task)
}
