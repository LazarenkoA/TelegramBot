package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	fresh "github.com/LazarenkoA/TelegramBot/Fresh"
	n "github.com/LazarenkoA/TelegramBot/Net"
	redis "github.com/LazarenkoA/TelegramBot/Redis"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
	"github.com/softlandia/cpd"
	"golang.org/x/text/encoding/charmap"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// Информация по заявкам хранится в Radis
// Структура хранения такая
//
// - инфо по заявкам СУИ:
// 			key - TicketID <идентификатор тикета>, value (hash) - TicketNumberm: <номер тикета>, ArticleID: <хз что, но это возвращатся в ответ при создании заявки>
// - Связь обновления и тикеты: key - <ref задания на обновления в агенте>, value - TicketID (один ко многим)
// - список активных тикетов СУИ, key - activeTickets
//
// Команды redis: https://redis.io/commands

type Ticket struct {
	Title        string
	Type         string
	Queue        string
	State        string
	Priority     string
	Service      string
	SLA          string
	Owner        string
	Responsible  string
	CustomerUser string
}
type Article struct {
	Subject     string
	Body        string
	ContentType string
}

type RequestDTO struct {
	UserLogin    string
	Password     string
	Ticket       *Ticket
	Article      *Article
	DynamicField []struct {
		Name  string
		Value string
	}
}

type TicketInfo struct {
	ArticleID    string `json:"ArticleID"`
	TicketNumber string `json:"TicketNumber"`
	TicketID     string `json:"TicketID"`
	Error        *struct {
		ErrorCode    string `json:"ErrorCode"`
		ErrorMessage string `json:"ErrorMessage"`
	} `json:"Error"`
}

type SUI struct {
	BaseTask
	GetListUpdateState

	respData   *TicketInfo
	fresh      *fresh.Fresh
	agent      string
	redis      *redis.Redis
	subject    string
	ticketBody string
}

//////////////////////////////////////////////////////////////////////////////////////////////////////

func (this *SUI) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.EndTask[reflect.TypeOf(this).String()] = []func(){finish}
	this.fresh = new(fresh.Fresh)
	Confs.DIContainer.Invoke(func(r *redis.Redis) {
		this.redis = r
	})

	agentStep := new(step).Construct("Выберите агент сервиса для получения списка заданий обновления", "choseAgent", this, ButtonCancel|ButtonBack, 2)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		Alias := conffresh.Alias
		conf := conffresh

		agentStep.appendButton(Alias, func() {
			this.ChoseAgent(Name)
			this.agent = Alias
			this.fresh.Conf = conf
			this.next("")
		})
	}
	agentStep.reverseButton()

	this.steps = []IStep{
		new(step).Construct("Что прикажете?", "start", this, ButtonCancel, 2).
			appendButton("Создать", func() {
				this.next("")
			}).
			appendButton("Завершить", func() {
				this.gotoByName("endTicket", "")
			}),
		//>> sumarokov
		new(step).Construct("Укажите тип заявки", "tickettype", this, ButtonCancel|ButtonBack, 2).
			appendButton("Произвольная заявка", func() {
				this.gotoByName("createArbitraryTicket", "")
			}).
			appendButton("На основе обновлений", func() {
				this.gotoByName("choseAgent", "")
			}).reverseButton(),
		//<< sumarokov
		agentStep,
		new(step).Construct("", "createTicket", this, ButtonCancel|ButtonBack, 3).
			whenGoing(func(thisStep IStep) {
				// исключаем те задания агента по которым уже создана заявка в СУИ
				for i := len(this.updateTask) - 1; i >= 0; i-- {
					if this.redis.KeyExists(this.updateTask[i].UUID) {
						this.updateTask = append(this.updateTask[:i], this.updateTask[i+1:]...)
					}
				}

				if len(this.updateTask) == 0 {
					thisStep.(*step).Buttons = []map[string]interface{}{}
					thisStep.(*step).txt = "Активных заданий на обновления не найдено"
					this.innerFinish()
				} else {
					thisStep.(*step).txt = fmt.Sprintf("Запланировано %v заданий на обновления, создать задачу в СУИ?", len(this.updateTask))
				}
				//thisStep.reverseButton()
			}).
			appendButton("Да", func() {
				basesFiltr := []string{}
				confinfo := map[string]string{}
				for _, v := range this.updateTask {
					basesFiltr = append(basesFiltr, v.Base)
					confinfo[v.Conf] = v.ToVersion
				}

				var bases = []*Bases{}
				var groupByConf = map[string][]*Bases{}
				if err := this.JsonUnmarshal(this.fresh.GetDatabase(basesFiltr), &bases); err != nil {
					logrus.WithError(err).Error("Ошибка дессериализации списка баз")
					return
				} else {
					for _, v := range bases {
						key := fmt.Sprintf("%v (%v)", v.Conf, confinfo[v.Conf])
						if _, ok := groupByConf[key]; !ok {
							groupByConf[key] = []*Bases{}
						}
						groupByConf[key] = append(groupByConf[key], v)
					}
				}
				TaskBody := fmt.Sprintf("Обновление контура %q\n\nКонфигурации:\n", this.agent)
				for k, v := range groupByConf {
					TaskBody += fmt.Sprintf("\t- %v\n", k)
					for _, base := range v {
						TaskBody += fmt.Sprintf("\t\t* %v (%v)\n", base.Caption, base.Name)
					}
				}
				this.ticketBody = TaskBody
				this.subject = "Плановые обновления конфигурации ЕИС УФХД"
				if _, err := this.createTicket(); err != nil {
					this.gotoByName("end", "При создании таска в СУИ произошла ошибка")
				}
			}),
		new(step).Construct("Завершить", "endTicket", this, ButtonCancel, 3).whenGoing(func(thisStep IStep) {
			tickets := this.getTickets()
			thisStep.(*step).Buttons = []map[string]interface{}{} // очистка кнопок, нужно при возврата назад с последующего шага

			if len(tickets) == 0 {
				thisStep.(*step).txt = "Нет активных заявок СУИ"
				this.innerFinish()
			} else {
				thisStep.(*step).txt = "Завершить следующие заявки в СУИ:\n"
				for _, t := range tickets {
					thisStep.(*step).txt += t.TicketNumber + "\n"
					TicketID := t.TicketID
					thisStep.appendButton(t.TicketNumber, func() {
						this.completeTask(TicketID)
						this.gotoByName("endwithback", "Готоводело")
					})
				}
				thisStep.appendButton("Все", func() {
					for _, v := range tickets {
						this.completeTask(v.TicketID)
					}
					this.gotoByName("end", "Готоводело")
					this.innerFinish()
					finish()
				})
				thisStep.reverseButton()
			}
		}),
		new(step).Construct("", "endwithback", this, ButtonBack, 1),
		new(step).Construct("", "end", this, 0, 1),
		new(step).Construct("Введите текст заявки", "createArbitraryTicket", this, ButtonCancel|ButtonBack, 3).
			whenGoing(func(thisStep IStep) {
				this.BaseTask.hookInResponse = func(update *tgbotapi.Update) bool {
					minTicketBodyLen := 10

					if this.ticketBody = strings.Trim(this.GetMessage().Text, " "); len([]rune(this.ticketBody)) <= minTicketBodyLen {
						this.next(fmt.Sprintf("Слишком короткое описание заявки! Должно быть больше %d символов. ", minTicketBodyLen))
						return false
					}

					this.DeleteMsg(update.Message.MessageID)
					this.subject = "Плановые работы ЕИС УФХД"

					if _, err := this.createTicket(); err != nil {
						this.gotoByName("end", "При создании таска в СУИ произошла ошибка")
					}

					return true
				}
			}),
		//new(step).Construct("Будет создана заявка с описанием", "preCreateArbitraryTicket", this, ButtonCancel|ButtonBack, 3).
		//	appendButton("Создать" func() {
		//
		//	}),
	}

	this.AppendDescription(this.name)
	return this
}

func (this *SUI) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")
	this.steps[this.currentStep].invoke(&this.BaseTask)
}

func (this *SUI) InfoWrapper(task ITask) {
	this.info = "ℹ Данным заданиям можно создать тески в СУИ по запланированым работам связанным с обновлением системы."
	this.BaseTask.InfoWrapper(task)
}

func (this *SUI) createTask() error {
	logrus.WithField("task", this.GetDescription()).Debug("Создаем задачу в СУИ")
	//if len(this.updateTask) == 0 {
	//	return errors.New("Нет данных по обновлениям")
	//}

	//var groupByConf = map[string][]*UpdateData{}
	//for _, v := range this.updateTask {
	//	key := fmt.Sprintf("%v (%v)", v.Conf, v.ToVersion)
	//	if _, ok := groupByConf[key]; !ok {
	//		groupByConf[key] = []*UpdateData{}
	//	}
	//	groupByConf[key] = append(groupByConf[key], &v)
	//}

	// TODO

	body := RequestDTO{
		UserLogin: Confs.SUI.User,
		Password:  Confs.SUI.Pass,
		Ticket: &Ticket{
			Title:        this.subject,
			Type:         "Запрос на обслуживание",
			Queue:        "УПРАВЛЕНИЯ ФИНАНСОВО-ХОЗЯЙСТВЕННОЙ ДЕЯТЕЛЬНОСТИ (УФХД)",
			State:        "В работе",
			Priority:     "Приоритет 4 – низкий",
			Service:      "7.УФХД: Обслуживание системы ",
			SLA:          "7.УФХД: SLA (низкий приоритет)",
			Owner:        "3lpufhdnparma",
			Responsible:  "3lpufhdnparma",
			CustomerUser: "api_ufhd",
		},
		Article: &Article{
			Subject:     this.subject,
			Body:        this.ticketBody,
			ContentType: "text/plain; charset=utf8",
		},
		DynamicField: []struct {
			Name  string
			Value string
		}{
			{
				"TicketSource", "Web",
			},
			{
				"ProcessManagementProcessID", "Process-74a8bd3dd6515fb7d1faf68aa5d2d1d0",
			},
			{
				"ProcessManagementActivityID", "Activity-932c4c75e80f46f35ebc4c1e3e387915",
			},
		},
	}
	jsonResp, err := this.sendHTTPRequest(http.MethodPost, fmt.Sprintf("%v/Ticket", Confs.SUI.URL), body)
	if err == nil {
		if err = json.Unmarshal([]byte(jsonResp), &this.respData); err != nil {
			logrus.WithError(err).Error("Ошибка десириализации данных СУИ")
		} else if this.respData.Error != nil {
			logrus.WithError(fmt.Errorf("(%s) %s", this.respData.Error.ErrorCode, this.respData.Error.ErrorMessage)).Error("Ошибка при создании тикета в СУИ")
		} else {
			this.addRedis()
		}
	} else {
		logrus.WithError(err).Error("Произошла ошибка при отпраке запроса в СУИ")
	}

	return err
}

func (this *SUI) completeTask(TicketID string) {
	if TicketID == "" {
		return
	}
	logrus.WithField("task", this.GetDescription()).WithField("TicketData", this.respData).Debug("Удаляем задачу в СУИ")
	if !this.checkState(TicketID) {
		logrus.WithField("task", this.GetDescription()).WithField("TicketData", this.respData).Debug("Заявка уже закрыта")
		// удаляем из списка активных
		this.redis.DeleteItems("activeTickets", TicketID)
		return
	}

	// что б не описывать структуру, решил так
	body := map[string]interface{}{
		"UserLogin": Confs.SUI.User,
		"Password":  Confs.SUI.Pass,
		"TicketID":  TicketID,
		"Ticket": map[string]interface{}{
			"State": "Решение предоставлено",
			"PendingTime": map[string]interface{}{
				"Diff": "86400",
			},
		},
		"Article": map[string]interface{}{
			"Subject":     "Закрытие тикета",
			"Body":        "Работы произведены",
			"ContentType": "text/plain; charset=utf8",
		},
	}
	_, err := this.sendHTTPRequest(http.MethodPatch, fmt.Sprintf("%v/Ticket/%v", Confs.SUI.URL, TicketID), body)
	if err != nil {
		logrus.WithError(err).Error("Произошла ошибка при отпраке запроса в СУИ")
		this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка при отпраке запроса в СУИ:\n%v", err)))
	}

	// удаляем из списка активных
	this.redis.DeleteItems("activeTickets", TicketID)
}

func (this *SUI) getTickets() []*TicketInfo {
	result := []*TicketInfo{}
	activeTickets := this.redis.Items("activeTickets")
	for _, v := range activeTickets {
		ticket := this.redis.StringMap(v)
		result = append(result, &TicketInfo{
			ArticleID:    ticket["ArticleID"],
			TicketNumber: ticket["TicketNumber"],
			TicketID:     v,
		})
	}

	return result
}

func (this *SUI) deferredExecution(delay time.Duration, f func()) {
	<-time.After(delay)
	f()
}

func (this *SUI) checkState(TicketID string) bool {
	jsonTxt, _ := this.sendHTTPRequest(http.MethodGet, fmt.Sprintf("%v/TicketList?UserLogin=%v&Password=%v&TicketID=%v", Confs.SUI.URL, Confs.SUI.User, Confs.SUI.Pass, TicketID), nil)

	data := map[string]interface{}{}
	if err := json.Unmarshal([]byte(jsonTxt), &data); err != nil {
		return true
	}

	if v, ok := data["Ticket"]; ok {
		for _, item := range v.([]interface{}) {
			if item.(map[string]interface{})["TicketID"] != TicketID {
				continue
			}

			return item.(map[string]interface{})["State"] != "Решение предоставлено"
		}

	} else {
		return true
	}

	return true
}

func (this *SUI) sendHTTPRequest(method, url string, dto interface{}) (string, error) {
	logrus.WithField("dto", dto).WithField("url", url).Debug("Отправка запроса в СУИ")

	netU := new(n.NetUtility).Construct(url, "", "")
	if dto != nil {
		if body, err := json.Marshal(dto); err == nil {
			netU.Body = bytes.NewReader(body)
		} else {
			return "", err
		}
	}

	return netU.CallHTTP(method, time.Minute, nil)
}

func (this *SUI) addRedis() {
	if this.respData == nil {
		return
	}

	this.redis.Begin()
	logrus.WithField("data", this.respData).Debug("Добавляем данные по заявке в redis")

	// Данные по созданной заявке
	data := map[string]string{
		"TicketNumber": this.respData.TicketNumber,
		"ArticleID":    this.respData.ArticleID,
	}
	this.redis.SetMap(this.respData.TicketID, data)

	// связь обновление - тикет
	for _, v := range this.updateTask {
		this.redis.AppendItems(v.UUID, this.respData.TicketID)
	}

	// добавляем в список активных тикетов
	this.redis.AppendItems("activeTickets", this.respData.TicketID)
	this.redis.Commit()
}

func (this *SUI) createTicket() (string, error) {
	if err := this.createTask(); err == nil {
		if len(this.steps) > 0 {
			this.gotoByName("end", fmt.Sprintf("Создана заявка с номером %q", this.respData.TicketNumber))
		}

		ticketID := this.respData.TicketID
		go this.deferredExecution(time.Hour*2, func() {
			logrus.WithField("task", this.GetDescription()).
				WithField("ticketID", ticketID).
				Info("Удаленмие заявки в СУИ по таймауту")

			if this.redis.Count("activeTickets") == 0 {
				return
			}

			if this.bot != nil {
				this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Завершение заявки %q в СУИ по таймауту", ticketID)))
			}
			this.completeTask(ticketID)
			this.innerFinish()
		})
	} else {
		logrus.WithError(err).Error()
		return "", err
	}

	return this.respData.TicketNumber, nil
}

func (this *SUI) Daemon() {
	Confs.DIContainer.Invoke(func(r *redis.Redis) {
		this.redis = r
	})

	t := time.NewTicker(time.Minute * 5)
	defer t.Stop()

	for {
		msg := this.redis.LPOP("Alerts")
		for msg != "" {
			// например если делать запись через redis-cli, текст будет в кодировке CP866, если из других источников будут записи попадать, кодировка будет другой
			encoding := cpd.CodepageAutoDetect([]byte(msg))
			if encoding == cpd.CP866 {
				encoder := charmap.CodePage866.NewDecoder()
				var err error
				if msg, err = encoder.String(msg); err != nil {
					continue
				}
			}

			this.subject = "Устранение аварии"
			this.ticketBody = msg
			if TicketNumber, err := this.createTicket(); err == nil {
				logrus.WithField("TicketNumber", TicketNumber).Info("Создан таск в СУИ")
			} else {
				logrus.WithError(err).Error("Ошибка создания тикета в СУИ")
			}

			msg = this.redis.LPOP("Alerts")
		}

		<-t.C
	}
}
