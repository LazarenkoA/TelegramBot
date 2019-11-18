package telegram

import (
	conf "1C/Configuration"
	"1C/fresh"
	"fmt"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdateActualCFE struct {
	SetPlanUpdate   // из-за BuildButtonsByBase
	DeployExtension // из-за InvokeJobJenkins
}

func (this *IvokeUpdateActualCFE) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	//this.BaseTask.Initialise(bot, update, finish)
	this.DeployExtension.Initialise(bot, update, finish)

	//////////////////// Шаги //////////////////////////
	firstStep := new(step).Construct("Выберите менеджер сервиса из которого будет получено расширение", "IvokeUpdateActualCFE-1", this, ButtonCancel, 2)
	for _, conffresh := range Confs.FreshConf {
		Name := conffresh.Name // Обязательно через переменную, нужно для замыкания
		firstStep.appendButton(conffresh.Alias, func() { this.ChoseMC(Name) })
	}
	////////////////////////////////////////////////////

	this.steps = []IStep{
		firstStep,
		new(step).Construct("Выберите один из вариантов установки", "IvokeUpdateActualCFE-2", this, ButtonCancel|ButtonBack, 2).
			appendButton("Все расширения в одну базу", this.allExtToBase).appendButton("Одно расширение во все базы", this.extToBases).reverseButton(),
		new(step).Construct("Выберите расширение для установки", "IvokeUpdateActualCFE-3", this, ButtonCancel|ButtonBack, 2), // Кнопки потом добавятся
		new(step).Construct("Выберите базу данных", "IvokeUpdateActualCFE-3", this, ButtonCancel|ButtonBack, 3).reverseButton(),
		new(step).Construct("Отправляем задание в jenkins, установить монопольно?", "IvokeUpdateActualCFE-4", this, ButtonCancel|ButtonBack, 2).
			appendButton("Да", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, true); err == nil {
					this.next(status)
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.next(status)
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
			}).
			appendButton("Нет", func() {
				status := ""
				if err := this.InvokeJobJenkins(&status, false); err == nil {
					this.next(status)
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
				} else {
					this.next(status)
					this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
				}
				this.next(status)
			}).reverseButton(),
		new(step).Construct("", "IvokeUpdateActualCFE-5", this, 0, 2).whenGoing(func() { this.invokeEndTask("") }),
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

	var extensions = []conf.Extension{}
	this.JsonUnmarshal(this.fresh.GetAllExtension(), &extensions)

	// добавляем кнопки к сл. шагу, раньше не могли т.к. кнопки зависят от предыдущего шага, костыльно конечно смотрится
	this.steps[this.currentStep+1].(*step).Buttons = []map[string]interface{}{}
	this.steps[this.currentStep+1].(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
	for _, ext := range extensions {
		locExt := ext // Обязательно через переменную, нужно для замыкания
		this.steps[this.currentStep+1].appendButton(locExt.GetName(), func() {
			this.ChoseExt([]*conf.Extension{&locExt}, nil)
			this.skipNext() // перепрыгиваем т.к. сл. шаг эт к другой лог. ветки
		}).reverseButton()
	}

	this.next("")
}

func (this *IvokeUpdateActualCFE) allExtToBase() {
	defer func() {
		if err := recover(); err != nil {
			msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, msg))
			//this.invokeEndTask()
		}
	}()

	ChoseBD := func(Bases *Bases) {
		var extensions = []*conf.Extension{}
		this.JsonUnmarshal(this.fresh.GetExtensionByDatabase(Bases.UUID), &extensions)
		this.ChoseExt(extensions, Bases)
		this.next("")
	}

	this.steps[3].(*step).Buttons = []map[string]interface{}{}
	this.steps[3].(*step).addDefaultButtons(this, ButtonCancel|ButtonBack)
	txt := this.BuildButtonsByBase(this.fresh.GetDatabase(), this.steps[3], ChoseBD)
	this.steps[3].reverseButton()
	this.goTo(3, txt)
}

func (this *IvokeUpdateActualCFE) ChoseExt(extentions []*conf.Extension, Base *Bases) {
	this.extentions = extentions
	this.base = Base
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
