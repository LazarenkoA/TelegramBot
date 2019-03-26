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
	Ini(*tgbotapi.BotAPI, *tgbotapi.Update, func())
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
	tasks    map[int][]ITask
	passHash string
	allowed  map[int]bool
	timer    *time.Ticker
}

var (
	Confs *conf.CommonConf
)

func (B *Tasks) ReadSettings() {
	B.tasks = make(map[int][]ITask, 0)
	B.allowed = make(map[int]bool, 0)

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

func (B *Tasks) Authentication(User *tgbotapi.User, pass string) (bool, string) {
	//logrus.Debug("Авторизация")

	UserID := User.ID
	comment := ""
	if B.allowed[UserID] {
		return true, comment
	} else {
		B.allowed[UserID] = GetHash(pass) == B.GetPss()
		if B.allowed[UserID] {
			comment = "Пароль верный"

			if B.timer == nil {
				B.timer = time.NewTicker(time.Hour)
				go func() {
					for range B.timer.C {
						B.allowed[UserID] = false
					}
				}()
			}
		} else {
			comment = "Пароль неправильный"
		}

		logrus.WithFields(logrus.Fields{
			"Авторизация":  comment,
			"Пользователь": User.UserName,
			"Имя":          User.FirstName,
			"Фамилия":      User.LastName,
		}).Info()
	}

	return B.allowed[UserID], comment
}

func (B *Tasks) ExecuteHook(update *tgbotapi.Update, UserID int) bool {
	result := false
	for _, t := range B.tasks[UserID] {
		if hook := t.GetHook(); hook != nil {
			result = true
			if hook(update) {
				t.RestHook()
			}
		}
	}

	return result
}

func (B *Tasks) CreateTask(task ITask, name string, UserID int, reUse bool) ITask {
	UUID, _ := uuid.NewV4()

	// Некоторые задания имеет смысл переиспользовать
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
	description    string
	bot            *tgbotapi.BotAPI
	update         *tgbotapi.Update
	hookInResponse func(*tgbotapi.Update) bool
	outFinish      func()
	state          int
	UUID           *uuid.UUID
}

func (B *BaseTask) AppendDescription(txt string) {
	B.description += txt + "\n"
}

func (B *BaseTask) Cancel() {
	B.state = StateDone
	B.bot.Send(tgbotapi.NewMessage(B.GetMessage().Chat.ID, "Задание отменено.\n"+B.description))
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

func (B *BaseTask) Ini(name string) {
	B.UUID, _ = uuid.NewV4()
	B.name = name
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
		Buttons = append(Buttons, tgbotapi.NewInlineKeyboardButtonData("Прервать", UUID.String()))
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
