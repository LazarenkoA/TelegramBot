package telegram

import (
	conf "1C/Configuration"
	"1C/fresh"
	"fmt"

	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

type IvokeUpdateActualCFE struct {
	SetPlanUpdate
	DeployExtension
	BuilAndUploadCfe
}

func (this *IvokeUpdateActualCFE) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) ITask {
	this.BaseTask.Initialise(bot, update, finish)

	this.fresh = new(fresh.Fresh)
	this.DeployExtension.Initialise(bot, update, finish) // так надо, особенность сложного наследования
	// у предка переопределяем события окончания выполнения, т.к. именно в методе предка конец
	this.DeployExtension.EndTask = []func(){}
	this.DeployExtension.EndTask = append(this.DeployExtension.EndTask, this.innerFinish)
	this.AppendDescription(this.name)

	return this
}

func (this *IvokeUpdateActualCFE) ChoseMC(ChoseData string) {
	defer func() {
		if err := recover(); err != nil {
			Msg := fmt.Sprintf("Произошла ошибка при выполнении %q: %v", this.name, err)
			this.baseFinishMsg(Msg)
		}
	}()

	logrus.WithField("MS", ChoseData).Debug("Вызов метода выбора МС")

	for _, conffresh := range Confs.FreshConf {
		if ChoseData == conffresh.Name {
			this.fresh.Conf = conffresh
			break
		}
	}

	msg := tgbotapi.NewMessage(this.ChatID, "Выберите один из вариантов установки")
	Buttons := make([]map[string]interface{}, 0, 0)
	this.appendButton(&Buttons, "Все расширения в одну базу", this.allExtToBase)
	this.appendButton(&Buttons, "Одно расширение во все базы", this.extToBases)
	this.createButtons(&msg, Buttons, 1, true)
	this.bot.Send(msg)
}

func (this *IvokeUpdateActualCFE) extToBases() {
	var extensions = []conf.Extension{}
	this.JsonUnmarshal(this.fresh.GetAllExtension(), &extensions)

	msg := tgbotapi.NewMessage(this.ChatID, "Выберите расширение для установки")
	Buttons := make([]map[string]interface{}, 0, 0)

	for _, ext := range extensions {
		locExt := ext // Обязательно через переменную, нужно для замыкания
		this.appendButton(&Buttons, locExt.GetName(), func() { this.ChoseExt([]*conf.Extension{&locExt}, nil) })
	}
	this.createButtons(&msg, Buttons, 2, true)
	this.bot.Send(msg)
}

func (this *IvokeUpdateActualCFE) allExtToBase() {
	ChoseBD := func(Bases *Bases) {
		var extensions = []*conf.Extension{}
		this.JsonUnmarshal(this.fresh.GetExtensionByDatabase(Bases.UUID), &extensions)
		this.ChoseExt(extensions, Bases)
	}

	this.BuildButtonsByBase(this.fresh.GetDatabase(), ChoseBD, nil)
}

func (this *IvokeUpdateActualCFE) ChoseExt(extentions []*conf.Extension, Base *Bases) {
	// Вопрос как устанавливать, монопольно или нет
	msg := tgbotapi.NewMessage(this.ChatID, "Отправляем задание в jenkins, установить монопольно?")
	Buttons := make([]map[string]interface{}, 0)
	this.appendButton(&Buttons, "Да", func() {
		if err := this.InvokeJobJenkins(extentions, Base, true); err == nil {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
		}
	})
	this.appendButton(&Buttons, "Нет", func() {
		if err := this.InvokeJobJenkins(extentions, Base, false); err == nil {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, "Задание отправлено в jenkins"))
		} else {
			this.bot.Send(tgbotapi.NewMessage(this.ChatID, fmt.Sprintf("Произошла ошибка:\n %v", err)))
		}
	})

	this.createButtons(&msg, Buttons, 2, true)
	this.bot.Send(msg)
}

func (this *IvokeUpdateActualCFE) Start() {
	logrus.WithField("description", this.GetDescription()).Debug("Start")
	// 1. выбираем МС
	// 2. выбираем расширение

	// Для выбора МС вызываем Start предка (BuilAndUploadCfe)
	this.BuilAndUploadCfe.Initialise(this.bot, this.update, this.outFinish)
	this.BuilAndUploadCfe.OverriteChoseMC = this.ChoseMC
	this.BuilAndUploadCfe.callback = this.callback // что бы у предка использовались данные потомка
	this.BuilAndUploadCfe.Start()
}

func (this *IvokeUpdateActualCFE) innerFinish() {
	this.baseFinishMsg(fmt.Sprintf("Задание:\n%v\nГотово!", this.GetDescription()))
	this.outFinish()
}

func (B *IvokeUpdateActualCFE) InfoWrapper(task ITask) {
	B.info = "ℹ️ Команда инициирует обновления рсширений через jenkins. " +
		"Задание в jenkins - update-cfe. Будет установлено актуальное на текущий момент расширение."
	B.BaseTask.InfoWrapper(task)
}
