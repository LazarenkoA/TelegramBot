package telegram

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	conf "github.com/LazarenkoA/TelegramBot/Configuration"
	fresh "github.com/LazarenkoA/TelegramBot/Fresh"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdateActualCFE struct {
	SetPlanUpdate   // из-за BuildButtonsByBase
	DeployExtension // из-за InvokeJobJenkins

	extensions []conf.Extension

	exclusiveInstall bool // монопольная установка
	onlyPatches      bool // установка всех расширений вендора
}

func (this *IvokeUpdateActualCFE) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)
	this.DeployExtension.Initialise(bot, update, finish)
	this.availablebases = make(map[string]Bases)

	MessagesID := 0
	this.steps = []IStep{
		new(step).Construct("Выберите менеджер сервиса из которого будет получено расширение", "IvokeUpdateActualCFE-1", this, ButtonCancel, 2).
			whenGoing(func(thisStep IStep) {
				thisStep.(*step).Buttons = []map[string]interface{}{}
				thisStep.(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
				for _, conffresh := range Confs.FreshConf {
					Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
					thisStep.appendButton(conffresh.Alias, func() { this.ChoseMC(Name) })
				}
				thisStep.reverseButton()
			}),
		new(step).Construct("Выберите один из вариантов установки", "IvokeUpdateActualCFE-2", this, ButtonCancel|ButtonBack, 2).
			appendButton("Все подходящие расширения", func() { this.gotoByName("IvokeUpdateActualCFE-ChooseIB") }). // прыгаем на шаг
			appendButton("Расширения вендора", func() {
				this.onlyPatches = true
				this.gotoByName("IvokeUpdateActualCFE-ChooseIB")
			}). // прыгаем на шаг выбора ИБ
			appendButton("Одно расширение в базы", func() { this.gotoByName("choseMode", "") }).reverseButton(),
		new(step).Construct("Отображать исправления вендора?", "choseMode", this, ButtonCancel|ButtonBack, 2).
			appendButton("Да", func() {
				this.extToBases(true)
			}).appendButton("Нет", func() {
			this.extToBases(false)
		}).reverseButton(),
		new(step).Construct("Выберите расширение для установки", "IvokeUpdateActualCFE-3", this, ButtonCancel|ButtonBack, 2).
			whenGoing(func(thisStep IStep) {
				thisStep.(*step).Buttons = []map[string]interface{}{}
				thisStep.(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
				for _, ext := range this.extensions {
					locExt := ext // Обязательно через переменную, нужно для замыкания
					thisStep.appendButton(fmt.Sprintf("%s (%s)", locExt.GetName(), locExt.Version), func() {
						this.ChoseExt([]*conf.Extension{&locExt}, nil)
						//this.skipNext() // перепрыгиваем т.к. сл. шаг эт к другой логической ветки
						this.gotoByName("IvokeUpdateActualCFE-ChooseIB")
					})
				}
				thisStep.reverseButton()
			}),
		new(step).Construct("Выберите базу данных", "IvokeUpdateActualCFE-ChooseIB", this, ButtonCancel|ButtonBack, 3).
			whenGoing(func(thisStep IStep) {
				MessagesID = this.GetMessage().MessageID
				JSON_Base := this.fresh.GetDatabase(nil)

				selected := []*Bases{}
				names := []string{}
				UUIDs := []string{}
				var msg tgbotapi.Message

				onlyExt := len(this.extentions) != 0 // Если попадаем сюда через выбор "Одно расширение в базы" расширение уже будет выбрано
				start := func() {
					if !onlyExt { // если ставим все расширения в базу
						var extensions = []*conf.Extension{}
						this.JsonUnmarshal(this.fresh.GetExtensionByDatabase(strings.Join(UUIDs, ","), this.onlyPatches), &extensions)
						this.ChoseExt(extensions, selected)
					} else {
						this.ChoseExt(this.extentions, selected)
					}
					if this.onlyPatches {
						// перепрыгиваем вопрос о монопольности т.к. патчи всегда немонопольно
						this.gotoByName("IvokeUpdateActualCFE-5")
					} else {
						this.gotoByName("IvokeUpdateActualCFE-4")
					}
				}

				ChoseBD := func(bases *Bases) {
					if bases == nil {
						var allbases = []*Bases{}
						this.JsonUnmarshal(JSON_Base, &allbases)
						for _, b := range allbases {
							UUIDs = append(UUIDs, b.UUID)
						}
						start()
						return
					}

					// Исключаем дубли
					exist := false
					for _, b := range selected {
						if b.UUID == bases.UUID {
							exist = true
							break
						}
					}
					if exist {
						return
					}
					selected = append(selected, bases)
					names = append(names, bases.Name)
					UUIDs = append(UUIDs, bases.UUID)

					txt := fmt.Sprintf("Для установки расширений выбрано %v баз:\n"+
						"%v", len(selected), strings.Join(names, "\n"))
					Buttons := make([]map[string]interface{}, 0, 0)
					this.appendButton(&Buttons, "Начать", start)

					if msg.MessageID == 0 {
						M := tgbotapi.NewMessage(this.ChatID, txt)
						this.createButtons(&M, Buttons, 1, false)
						msg, _ = this.bot.Send(M)
					} else {
						M := tgbotapi.NewEditMessageText(this.ChatID, msg.MessageID, txt)
						this.createButtons(&M, Buttons, 1, false)
						msg, _ = this.bot.Send(M)
					}

					if thisStep, ok := this.CurrentStep().(*step); ok {
						thisStep.nivigation = fmt.Sprintf("%v (%v)", thisStep.stepName, fmt.Sprintf("Выбрано %d", len(names)))
					}
				}

				thisStep.(*step).Buttons = []map[string]interface{}{}
				thisStep.(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
				txt := this.BuildButtonsByBase(JSON_Base, thisStep, ChoseBD, onlyExt || this.onlyPatches) // для расширений вендора имеет смысл отображать кнопку "все"
				thisStep.(*step).SetCaption(txt)
				thisStep.reverseButton()
			}),
		new(step).Construct("Установить монопольно?", "IvokeUpdateActualCFE-4", this, ButtonCancel, 2).
			whenGoing(func(thisStep IStep) {
				// Новое сообщение не всегда появляется, например если нажать на кнопку "все" сообщения не будет из-за этого эта проверка
				if MessagesID != this.GetMessage().MessageID {
					bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
						ChatID:    this.ChatID,
						MessageID: MessagesID})
				}
			}).
			appendButton("Да", func() {
				this.exclusiveInstall = true
				this.gotoByName("IvokeUpdateActualCFE-5")
			}).
			appendButton("Нет", func() {
				this.exclusiveInstall = false
				this.gotoByName("IvokeUpdateActualCFE-5")
			}).reverseButton(),
		new(step).Construct("Установить сейчас?", "IvokeUpdateActualCFE-5", this, ButtonCancel, 2).
			appendButton("Да", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, this.exclusiveInstall); err == nil {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.gotoByName("IvokeUpdateActualCFE-6", status)
			}).
			appendButton("Нет", func() {
				this.setDefferedUpdateCF()
			}).reverseButton(),
		new(step).Construct("Укажите через сколько минут необходимо запустить обновление.", "IvokeUpdateActualCFE-SetTime", this, 0, 2),
		new(step).Construct("Статус", "IvokeUpdateActualCFE-6", this, 0, 2),
	}

	this.AppendDescription(this.name)

	return this
}

func (this *IvokeUpdateActualCFE) ChoseMC(ChoseData string) {
	logrus.WithField("MS", ChoseData).Debug("Вызов метода выбора МС")

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			this.fresh = new(fresh.Fresh).Construct(conffresh)
			break
		}
	}

	this.next("")
}

func (this *IvokeUpdateActualCFE) extToBases(vendorPatch bool) {
	defer func() {
		if err := recover(); err != nil {
			msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, msg))
			//this.invokeEndTask()
		}
	}()

	filter := fresh.ExtAll
	if !vendorPatch {
		filter = fresh.ExtWithOutPatch
	}
	this.JsonUnmarshal(this.fresh.GetAllExtension(filter), &this.extensions)
	sort.Slice(this.extensions, func(i, j int) bool {
		array := []string{this.extensions[i].Name, this.extensions[j].Name}
		sort.Strings(array)
		return array[0] == this.extensions[i].Name
	})
	this.next("")
}

func (this *IvokeUpdateActualCFE) setDefferedUpdateCF() {
	var shiftMin int

	defer func() {
		if err := recover(); err != nil {
			msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, msg))
		}
	}()

	this.gotoByName("IvokeUpdateActualCFE-SetTime")

	this.hookInResponse = func(update *tgbotapi.Update) (result bool) {
		var err error
		var status string

		this.DeleteMsg(update.Message.MessageID)
		if shiftMin, err = strconv.Atoi(this.GetMessage().Text); err != nil {
			this.gotoByName("IvokeUpdateActualCFE-SetTime", fmt.Sprintf("Введите число. Вы ввели %q", this.GetMessage().Text))
			return false
		}

		this.gotoByName("IvokeUpdateActualCFE-SetTime", fmt.Sprintf("Задание будет выполнено через %d мин.", shiftMin))
		go this.deferredExecution(time.Minute*time.Duration(shiftMin), func() {

			msgtext := fmt.Sprintf("Запущена отложенная на %d мин. установка расширений", shiftMin)
			msg := tgbotapi.NewMessage(this.GetChatID(), "")
			msg.ReplyToMessageID = this.CurrentStep().GetMessageID()

			if err := this.InvokeJobJenkins(&status, this.exclusiveInstall); err == nil {
				msg.Text = msgtext + "\nЗадание отправлено в jenkins."
			} else {
				msg.Text = fmt.Sprintf("%s\nПроизошла ошибка:\n %v", msgtext, err)
				logrus.WithError(err).Error("Произошла ошибка при запуске отложенной установки расширений")
			}

			this.bot.Send(msg)
			logrus.Info(msgtext)
			this.gotoByName("IvokeUpdateActualCFE-6", status)
		})

		return true
	}

}

func (this *IvokeUpdateActualCFE) deferredExecution(delay time.Duration, f func()) {
	<-time.After(delay)
	f()
}

func (this *IvokeUpdateActualCFE) ChoseExt(extentions []*conf.Extension, Base []*Bases) {
	this.extentions = extentions
	for _, b := range Base {
		this.availablebases[b.UUID] = *b
	}
}

func (this *IvokeUpdateActualCFE) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")

	this.CurrentStep().invoke(&this.BaseTask)
}

func (B *IvokeUpdateActualCFE) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда инициирует обновления рсширений через jenkins. " +
		"Задание в jenkins - update-cfe. Будет установлено актуальное на текущий момент расширение."
	B.BaseTask.InfoWrapper(task)
}
