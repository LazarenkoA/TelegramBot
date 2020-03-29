package telegram

import (
	conf "TelegramBot/Configuration"
	"TelegramBot/fresh"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdateActualCFE struct {
	SetPlanUpdate   // из-за BuildButtonsByBase
	DeployExtension // из-за InvokeJobJenkins

	extensions []conf.Extension
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
			appendButton("Все подходящие расширения", func() { this.goTo(3, "") }). // прыгаем на 3й шаг
			appendButton("Одно расширение в базы", this.extToBases).reverseButton(),
		new(step).Construct("Выберите расширение для установки", "IvokeUpdateActualCFE-3", this, ButtonCancel|ButtonBack, 2).
			whenGoing(func(thisStep IStep) {
				thisStep.(*step).Buttons = []map[string]interface{}{}
				thisStep.(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
				for _, ext := range this.extensions {
					locExt := ext // Обязательно через переменную, нужно для замыкания
					thisStep.appendButton(locExt.GetName(), func() {
						this.ChoseExt([]*conf.Extension{&locExt}, nil)
						//this.skipNext() // перепрыгиваем т.к. сл. шаг эт к другой логической ветки
						this.next("")
					})
				}
				thisStep.reverseButton()
			}),
		new(step).Construct("Выберите базу данных", "IvokeUpdateActualCFE-3", this, ButtonCancel|ButtonBack, 3).
			whenGoing(func(thisStep IStep) {
				MessagesID = this.GetMessage().MessageID

				selected := []*Bases{}
				names := []string{}
				var msg tgbotapi.Message

				onlyExt := len(this.extentions) != 0 // Если попадаем сюда через выбор "Одно расширение в базы" расшимрение уже будет выбрано

				ChoseBD := func(Bases *Bases) {
					if Bases == nil {
						this.ChoseExt(this.extentions, nil)
						this.next("")
						return
					}

					// Исключаем дубли
					exist := false
					for _, b := range selected {
						if b.UUID == Bases.UUID {
							exist = true
							break
						}
					}
					if exist {
						return
					}
					selected = append(selected, Bases)
					names = append(names, Bases.Name)

					txt := fmt.Sprintf("Для установки расширений выбрано %v баз:\n" +
						"%v", len(selected), strings.Join(names, "\n"))
					Buttons := make([]map[string]interface{}, 0, 0)
					this.appendButton(&Buttons, "Начать", func() {

						if !onlyExt {
							var extensions = []*conf.Extension{}
							this.JsonUnmarshal(this.fresh.GetExtensionByDatabase(Bases.UUID), &extensions)
							this.ChoseExt(extensions, selected)
						} else {
							this.ChoseExt(this.extentions, selected)
						}
						this.next("")
					})

					if msg.MessageID == 0 {
						M := tgbotapi.NewMessage(this.ChatID, txt)
						this.createButtons(&M, Buttons, 1, false)
						msg, _ = this.bot.Send(M)
					} else {
						M := tgbotapi.NewEditMessageText(this.ChatID, msg.MessageID, txt)
						this.createButtons(&M, Buttons, 1, false)
						msg, _ = this.bot.Send(M)
					}
				}

				thisStep.(*step).Buttons = []map[string]interface{}{}
				thisStep.(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
				txt := this.BuildButtonsByBase(this.fresh.GetDatabase(), thisStep, ChoseBD, onlyExt)
				thisStep.(*step).SetCaption(txt)
				thisStep.reverseButton()
			}),
		new(step).Construct("Отправляем задание в jenkins, установить монопольно?", "IvokeUpdateActualCFE-4", this, ButtonCancel, 2).
			whenGoing(func(thisStep IStep) {
				bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
				ChatID:    this.ChatID,
				MessageID: MessagesID})
			}).
			appendButton("Да", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, true); err == nil {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.next(status) // завершится в DeployExtension
			}).
			appendButton("Нет", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, false); err == nil {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.next(status)
			}).reverseButton(),
		new(step).Construct("Статус", "IvokeUpdateActualCFE-5", this, 0, 2),
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

func (this *IvokeUpdateActualCFE) extToBases() {
	defer func() {
		if err := recover(); err != nil {
			msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, msg))
			//this.invokeEndTask()
		}
	}()

	this.extensions = []conf.Extension{}
	this.JsonUnmarshal(this.fresh.GetAllExtension(), &this.extensions)
	this.next("")
}

func (this *IvokeUpdateActualCFE) ChoseExt(extentions []*conf.Extension, Base []*Bases) {
	this.extentions = extentions
	for _, b := range Base {
		this.availablebases[b.UUID] = *b
	}
}

func (this *IvokeUpdateActualCFE) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")

	this.steps[this.currentStep].invoke(&this.BaseTask)
}

func (B *IvokeUpdateActualCFE) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда инициирует обновления рсширений через jenkins. " +
		"Задание в jenkins - update-cfe. Будет установлено актуальное на текущий момент расширение."
	B.BaseTask.InfoWrapper(task)
}
