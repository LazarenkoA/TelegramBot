package telegram

import (
	conf "1C/Configuration"
	settings "1C/Confs"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/garyburd/redigo/redis"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/sirupsen/logrus"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
)

const (
	StateNew int = iota
	StateWork
	StateDone
)

type ITask interface {
	Initialise(*tgbotapi.BotAPI, tgbotapi.Update, func()) ITask
	Start()
	InfoWrapper(ITask)
	GetCallBack() map[string]func()
	GetHook() func(*tgbotapi.Update) bool
	RestHook()
	GetName() string
	GetState() int
	GetUUID() *uuid.UUID
	SetUUID(*uuid.UUID)
	SetName(string)
	//isDone() bool
}

type Tasks struct {
	tasks       map[int][]ITask
	passHash    string
	timer       map[int]*time.Ticker
	SessManager *settings.SessionManager
}

type Cluster struct {
	MainServer string `json:"MainServer"`
	RASServer  string `json:"RASServer"`
	RASPort    int    `json:"RASPort"`
}

type Bases struct {
	Caption  string   `json:"Caption"`
	Name     string   `json:"Name"`
	UUID     string   `json:"UUID"`
	UserName string   `json:"UserName"`
	UserPass string   `json:"UserPass"`
	Cluster  *Cluster `json:"Cluster"`
	URL      string   `json:"URL"`
}

var (
	Confs *conf.CommonConf
)

func (B *Tasks) ReadSettings() {
	B.tasks = make(map[int][]ITask, 0)
	B.timer = make(map[int]*time.Ticker, 0)

	currentDir, _ := os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "Common.conf")

	Confs = new(conf.CommonConf)
	settings.ReadSettings(CommonConfPath, Confs)
}

func (B *Tasks) GetPss() string {
	if B.passHash != "" {
		return B.passHash
	}

	currentDir, _ := os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "pass")

	if _, err := os.Stat(CommonConfPath); os.IsNotExist(err) {
		logrus.WithField("файл", CommonConfPath).Panic("Файл с паролем не найден. Воспользуйтесь ключем запуска SetPass")
		return ""
	}

	file, err := ioutil.ReadFile(CommonConfPath)
	if err != nil {
		logrus.WithField("файл", CommonConfPath).WithField("Ошибка", err).Panic("Ошибка открытия файла")
		return ""
	}

	B.passHash = string(file)
	return B.passHash
}

func (B *Tasks) SetPass(pass string) error {
	B.passHash = GetHash(pass)

	currentDir, _ := os.Getwd()
	CommonConfPath := filepath.Join(currentDir, "Confs", "pass")
	err := ioutil.WriteFile(CommonConfPath, []byte(B.passHash), os.ModeExclusive)
	if err != nil {
		logrus.WithField("файл", CommonConfPath).WithField("Ошибка", err).Panic("Ошибка записи файла")
		return err
	}

	return nil
}

func (B *Tasks) CheckSession(User *tgbotapi.User, pass string) (bool, string) {
	//logrus.Debug("Авторизация")

	if B.SessManager == nil {
		return false, "Не задан менеджер сессии"
	}

	if passCash, err := B.SessManager.GetSessionData(User.ID); err == nil {
		if passCash == B.GetPss() {
			return true, ""
		} else {
			B.SessManager.DeleteSessionData(User.ID)
			return false, "В кеше не верный пароль"
		}
	} else if err == redis.ErrNil {
		// в кеше нет данных
		logrus.WithFields(logrus.Fields{
			"Пользователь": User.UserName,
			"Имя":          User.FirstName,
			"Фамилия":      User.LastName,
		}).Info("Попытка авторизации")

		if GetHash(pass) == B.GetPss() {
			if err := B.SessManager.AddSessionData(User.ID, GetHash(pass)); err != nil {
				return false, err.Error()
			}
			return true, "Пароль верный"
		}
	} else {
		return false, err.Error()
	}

	return false, "Пароль не верный"
}

func (B *Tasks) ExecuteHook(update tgbotapi.Update, UserID int) bool {
	result := false
	for _, t := range B.tasks[UserID] {
		if hook := t.GetHook(); hook != nil {
			result = true
			if hook(&update) {
				t.RestHook()
			}
		}
	}

	return result
}

func (B *Tasks) AppendTask(task ITask, name string, UserID int, reUse bool) ITask {
	UUID, _ := uuid.NewV4()

	// Некоторые задания имеет смысл переиспользовать, например при получении списка заданий агента, что бы при повторном запросе видно было какие отслеживаются, а какие нет.
	if reUse {
		for _, t := range B.GetTasks(UserID) {
			if t.GetName() == name {
				return t
			}
		}
	}

	task.SetName(name)
	task.SetUUID(UUID)
	B.Append(task, UserID)

	return task
}

func (B *Tasks) Append(t ITask, UserID int) error {
	/* for _, item := range B.tasks[UserID] {
		if item.GetName() == t.GetName() && item.GetState() != StateDone {
			return fmt.Errorf("Задание %q уже выполняется", t.GetName())
		}
	} */
	B.tasks[UserID] = append(B.tasks[UserID], t)
	return nil
}

func (B *Tasks) Delete(UserID int) {
	for i := len(B.tasks[UserID]) - 1; i >= 0; i-- {
		if B.tasks[UserID][i].GetState() == StateDone {
			B.tasks[UserID] = append(B.tasks[UserID][:i], B.tasks[UserID][i+1:]...)
		}
	}
}

func (B *Tasks) GetTasks(UserID int) []ITask {
	return B.tasks[UserID]
}

func (B *Tasks) Reset(fromID int, bot *tgbotapi.BotAPI, update *tgbotapi.Update, clear bool) {
	if clear {
		B.clearTasks(fromID)
	}

	/* bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "Вот что я умею:\nСобрать файл конфигурации cf /BuildCf\n\n"+
	"Собрать файлы расширений cfe /BuildCfe\n\n"+
	"Собрать конфигурацию и отправить во фреш /BuilAndUploadCf\n\n"+
	"Собрать Файлы расширений и обновить во фреше /BuilAndUploadCfe\n\n"+
	"Запланитьвать обновление /SetPlanUpdate\n\n"+
	"Получить список запланированных обновлений конфигураций /GetListUpdateState\n\n"+
	"Отмена текущего действия /Cancel")) */
}

func (B *Tasks) clearTasks(fromID int) {
	B.tasks[fromID] = make([]ITask, 0, 0)
}

//////////////////////// Common ////////////////////////

func GetHash(pass string) string {
	first := sha256.New()
	first.Write([]byte(pass))

	return fmt.Sprintf("%x", first.Sum(nil))
}

//////////////////////// Base struct ////////////////////////

type BaseTask struct {
	name           string
	callback       map[string]func()
	key            string
	description    map[string]bool // map для уникальности
	bot            *tgbotapi.BotAPI
	update         *tgbotapi.Update
	hookInResponse func(*tgbotapi.Update) bool
	outFinish      func()
	state          int
	UUID           *uuid.UUID
	info           string
}

func (B *BaseTask) Initialise(bot *tgbotapi.BotAPI, update *tgbotapi.Update, finish func()) {
	B.bot = bot
	B.update = update
	B.outFinish = finish
	B.state = StateWork
}

func (B *BaseTask) Continue(task ITask) {
	task.Start()
}

func (B *BaseTask) InfoWrapper(task ITask) {
	Buttons := make([]map[string]interface{}, 0)
	B.appendButton(&Buttons, "✅ Продолжить", func() { B.Continue(task) })

	msg := tgbotapi.NewMessage(B.GetMessage().Chat.ID, B.info)
	B.createButtons(&msg, Buttons, 2, true)
	B.bot.Send(msg)
}

func (B *BaseTask) AppendDescription(txt string) {
	if B.description == nil {
		B.description = make(map[string]bool, 0)
	}

	B.description[txt] = true
}
func (B *BaseTask) GetDescription() (result string) {
	for v, _ := range B.description {
		result += v + "\n"
	}
	return result
}

func (B *BaseTask) Cancel() {
	B.state = StateDone
	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Задание отменено.\n"+B.GetDescription()))
}

func (B *BaseTask) breakButtonsByColum(Buttons []tgbotapi.InlineKeyboardButton, countColum int) [][]tgbotapi.InlineKeyboardButton {
	end := 0
	result := [][]tgbotapi.InlineKeyboardButton{}

	for i := 1; i <= int(float64(len(Buttons)/countColum)); i++ {
		end = i * countColum
		start := (i - 1) * countColum
		if end > len(Buttons) {
			end = len(Buttons)
		}

		row := tgbotapi.NewInlineKeyboardRow(Buttons[start:end]...)
		result = append(result, row)
	}
	if len(Buttons)%countColum > 0 {
		row := tgbotapi.NewInlineKeyboardRow(Buttons[end:len(Buttons)]...)
		result = append(result, row)
	}

	return result
}

func (B *BaseTask) GetName() string {
	return B.name
}

func (B *BaseTask) GetUUID() *uuid.UUID {
	return B.UUID
}

func (B *BaseTask) GetKey() string {
	return B.key
}

func (B *BaseTask) GetHook() func(*tgbotapi.Update) bool {
	return B.hookInResponse
}

func (B *BaseTask) RestHook() {
	B.hookInResponse = nil
}

func (B *BaseTask) GetCallBack() map[string]func() {
	return B.callback
}

func (B *BaseTask) baseFinishMsg(str string) {
	B.state = StateDone
	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, str))
}

func (B *BaseTask) GetState() int {
	return B.state
}

func (B *BaseTask) JsonUnmarshal(JSON string, v interface{}) {
	if JSON == "" {
		panic("JSON пустой")
	}

	err := json.Unmarshal([]byte(JSON), v)
	if err != nil {
		logrus.WithField("JSON", JSON).Debug()
		panic(fmt.Errorf("Ошибка разпаковки JSON: %v", err))
	}
}

func (B *BaseTask) GetMessage() *tgbotapi.Message {
	var Message *tgbotapi.Message

	if B.update.CallbackQuery != nil {
		Message = B.update.CallbackQuery.Message
	} else {
		Message = B.update.Message
	}

	return Message
}

func (B *BaseTask) createButtons(Msg *tgbotapi.MessageConfig, data []map[string]interface{}, countColum int, addCancel bool) {
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	if B.callback == nil {
		B.callback = make(map[string]func(), 0)
	}
	for _, item := range data {
		ID := item["ID"].(string)
		if _, ok := B.callback[ID]; ok {
			continue // если с таким id значит что-то не так
		}

		btn := tgbotapi.NewInlineKeyboardButtonData(item["Caption"].(string), ID)
		B.callback[ID] = item["Invoke"].(func())
		Buttons = append(Buttons, btn)
	}

	if addCancel {
		UUID, _ := uuid.NewV4()
		Buttons = append(Buttons, tgbotapi.NewInlineKeyboardButtonData("🚫 Прервать", UUID.String()))
		B.callback[UUID.String()] = B.Cancel
	}

	keyboard.InlineKeyboard = B.breakButtonsByColum(Buttons, countColum)
	Msg.ReplyMarkup = &keyboard
}

func (B *BaseTask) appendButton(Buttons *[]map[string]interface{}, Caption string, Invoke func()) {
	UUID, _ := uuid.NewV4()
	*Buttons = append(*Buttons, map[string]interface{}{
		"Caption": Caption,
		"ID":      UUID.String(),
		"Invoke":  Invoke,
	})
}

func (B *BaseTask) SetUUID(UUID *uuid.UUID) {
	B.UUID = UUID
}
func (B *BaseTask) SetName(name string) {
	B.name = name
}

//////////////////////// Task Factory ////////////////////////

type TaskFactory struct {
}

func (this *TaskFactory) BuilAndUploadCf() ITask {
	return new(BuilAndUploadCf)
}
func (this *TaskFactory) BuilAndUploadCfe() ITask {
	return new(BuilAndUploadCfe)
}
func (this *TaskFactory) BuildCf() ITask {
	object := new(BuildCf)
	object.AllowSaveLastVersion = true // Флаг для того что бы можно было сохранять версию -1, т.е. актуальную (не всегда эо нужно)
	return object
}
func (this *TaskFactory) BuildCfe() ITask {
	return new(BuildCfe)
}
func (this *TaskFactory) DeployExtension() ITask {
	return new(DeployExtension)
}
func (this *TaskFactory) GetListUpdateState() ITask {
	return new(GetListUpdateState)
}
func (this *TaskFactory) IvokeUpdate() ITask {
	return new(IvokeUpdate)
}
func (this *TaskFactory) SetPlanUpdate() ITask {
	return new(SetPlanUpdate)
}
func (this *TaskFactory) IvokeUpdateActualCFE() ITask {
	return new(IvokeUpdateActualCFE)
}
