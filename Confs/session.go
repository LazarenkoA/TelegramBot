package conf

import (
	"strconv"
	"time"

	"github.com/LazarenkoA/TelegramBot/Redis"
)

type SessionManager struct {
	redis *redis.Redis
}

func  (sm *SessionManager) NewSessionManager(redis *redis.Redis) *SessionManager {
	sm.redis = redis
	return sm
}

func (sm *SessionManager) AddSessionData(idSession int, data string) error {
	return sm.redis.Set(strconv.Itoa(idSession), data, time.Hour)
}

func (sm *SessionManager) GetSessionData(idSession int) (string, error) {
	return sm.redis.Get(strconv.Itoa(idSession))
}

func (sm *SessionManager) DeleteSessionData(idSession int) error {
	return sm.redis.Delete(strconv.Itoa(idSession))
}
