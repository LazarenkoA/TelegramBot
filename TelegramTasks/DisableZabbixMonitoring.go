package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	zabbix "github.com/LazarenkoA/go-zabbix"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
)

type DisableZabbixMonitoring struct {
	BaseTask

	zabbixSession *zabbix.Session
}

func (this *DisableZabbixMonitoring) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)

	if session, err := zabbix.NewSession(Confs.Zabbix.URL+"/api_jsonrpc.php", Confs.Zabbix.Login, Confs.Zabbix.Password); err != nil {
		logrus.WithError(err).Error("Произошла ошибка подключения к zabbix")
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Задание:\n%v\nПроизошла ошибка подключения к zabbix!\n%v", this.GetDescription(), err)))
		this.invokeEndTask("")
		return nil
	} else {
		this.zabbixSession = session
	}

	this.steps = []IStep{
		new(step).Construct("Укажите на сколько часов необходимо отключить мониторинг", "Шаг-1", this, ButtonCancel, 2),
		new(step).Construct("", "Шаг-2", this, ButtonCancel, 2),
	}

	this.AppendDescription(this.name)
	return this
}

func (this *DisableZabbixMonitoring) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")
	this.CurrentStep().invoke(&this.BaseTask)

	this.hookInResponse = func(update *tgbotapi.Update) bool {
		var hours int
		var err error
		if hours, err = strconv.Atoi(strings.Trim(this.GetMessage().Text, " ")); err != nil {
			this.DeleteMsg(update.Message.MessageID)
			this.next(fmt.Sprintf("Введите число. Вы ввели %q", this.GetMessage().Text))
			return false
		} else if hours == 0 {
			this.DeleteMsg(update.Message.MessageID)
			this.next("Количество часов не может быть равное 0")
			return false
		}

		this.disableMonitor(hours)
		return true
	}
}

func (this *DisableZabbixMonitoring) disableMonitor(hours int) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			logrus.Error(Msg)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, Msg))
		}
		this.invokeEndTask("")
	}()

	CParams := &zabbix.MaintenanceCreateParams{
		HostNames: []string{"ACCOUNTING.PERMRKAI.RU"},
		Timeperiods: []zabbix.Timeperiods{
			zabbix.Timeperiods{
				Period: int(time.Duration(time.Hour * time.Duration(hours)).Seconds()),
			},
		},
	}

	this.deleteMaintenance()

	m := new(zabbix.Maintenance)
	m.Name = "AutoCreated"
	m.ActiveSince = time.Now()
	m.ServicePeriod = hours
	if err := this.zabbixSession.CreateMaintenance(CParams.FillFields(m)); err != nil {
		panic(err)
	}

	// по истичению времени удаляем обслуживание
	timer := time.NewTicker(time.Hour * time.Duration(hours))
	go func() {
		<-timer.C
		timer.Stop()
		this.deleteMaintenance()
	}()
}

func (this *DisableZabbixMonitoring) InfoWrapper(task ITask) {
	this.info = "ℹ Отключение мониторинга zabbix."
	this.BaseTask.InfoWrapper(task)
}

func (this *DisableZabbixMonitoring) deleteMaintenance() {
	getParams := &zabbix.MaintenanceGetParams{}
	getParams.TextSearch = map[string]string{"name": "AutoCreated"}
	if maintenance, err := this.zabbixSession.GetMaintenance(getParams); err == nil && len(maintenance) == 1 {
		logrus.Debug(`Удаляем период обслуживания "AutoCreated"`)
		maintenance[0].Delete(this.zabbixSession)
	}
}
