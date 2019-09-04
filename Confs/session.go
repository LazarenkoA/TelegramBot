package conf

import (
	"fmt"

	"github.com/garyburd/redigo/redis"
	"github.com/sirupsen/logrus"
)

const redisAddr = "redis://user:@localhost:6379/0"

// type SessionData struct {
// 	hashPass string
// }

type SessionManager struct {
	redisConn redis.Conn
}

func NewSessionManager() (result *SessionManager, err error) {
	var redisConn redis.Conn
	redisConn, err = redis.DialURL(redisAddr)
	if err != nil {
		logrus.WithField("redisAddr", redisAddr).Panic("Ошибка установки соединения с redis. Проверьте, что служба redis запущена")
	}

	return &SessionManager{redisConn: redisConn}, err
}

func (sm *SessionManager) AddSessionData(idSession int, data string) error {
	outdata, err := sm.redisConn.Do("SET", idSession, data, "EX", 3600)
	result, err := redis.String(outdata, err)
	if err != nil {
		logrus.Error(err)
		return err
	}
	if result != "OK" {
		return fmt.Errorf("Redis. result not OK")
	}
	return nil
}

func (sm *SessionManager) GetSessionData(idSession int) (string, error) {
	data, err := redis.String(sm.redisConn.Do("GET", idSession))
	if err != nil {
		if err != redis.ErrNil { // ErrNil не логируем ибо нефиг засорять логи )
			logrus.Error(err)
		}
		return "", err
	}
	return data, nil
}

func (sm *SessionManager) DeleteSessionData(idSession int) error {
	_, err := redis.Int(sm.redisConn.Do("DEL", idSession))
	if err != nil {
		logrus.Error(err)
		return fmt.Errorf("redis error: %v", err)
	}
	return nil
}
