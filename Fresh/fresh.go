package fresh

import (
	cf "1C/Configuration"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Fresh struct {
	Conf          *cf.FreshConf
	ConfCode      string
	ConfComment   string
	tempFile      string
	ConfFreshName string
	fileSize      int64
}

func (f *Fresh) upLoadFile(fileName string) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	logrus.WithFields(map[string]interface{}{
		"Сервис": f.Conf.SM.URL + f.Conf.SM.UpLoadFileServiceURL,
		"Login":  f.Conf.SM.Login,
		"Pass":   f.Conf.SM.Pass,
	}).Info("Загружаем файл во фреш")

	MByteCount := 5
	info, _ := os.Stat(fileName)
	LenBuf := (1024 * 1024) * MByteCount
	f.fileSize = info.Size()
	//SizeMb := info.Size() / 1024 / 1024

	logrus.WithFields(logrus.Fields{
		"Размер частей в Mb":           MByteCount,
		"Размер файла в byte":          f.fileSize,
		"Количество итераций загрузки": f.fileSize / int64(LenBuf),
	}).Debug("Информация о файле")

	buf := make([]byte, LenBuf)
	pos, err := file.Read(buf)
	for err != io.EOF {
		if e := f.sendByte(buf[:pos]); e != nil {
			return e
		}
		pos, err = file.Read(buf)
	}

	logrus.WithField("файл", fileName).Info("Файл загружен")
	return nil
}

func (f *Fresh) RegConfigurations(wg *sync.WaitGroup, chError chan error, filename string, callBack func()) {
	defer wg.Done()
	if callBack != nil {
		defer callBack()
	}
	defer func() {
		if err := recover(); err != nil {
			chError <- fmt.Errorf("Произошла ошибка при регистрации конфигурации в МС: %q", err)
		}
	}()

	logrus.WithField("файл", filename).Info("Отправляем конфигурацию во фреш")
	if err := f.upLoadFile(filename); err == nil {
		url := fmt.Sprintf("%v%v?FileName=%v&ConfCode=%v", f.Conf.SM.URL, f.Conf.SM.RegConfigurationServiceURL, f.tempFile, f.ConfCode)
		f.callServiceSM("GET", url, time.Minute*5)
	} else {
		panic(err) // в defer есть перехват
	}

	logrus.WithField("файл", filename).Info("Файл загружен")
}

func (f *Fresh) RegExtension(wg *sync.WaitGroup, chError chan<- error, filename string, callBack func()) {
	defer wg.Done()
	if callBack != nil {
		defer callBack()
	}
	defer func() {
		if err := recover(); err != nil {
			chError <- fmt.Errorf("Произошла ошибка при регистрации расширения %q в МС: %q", extName, err)
		}
	}()

	logrus.WithField("файл", filename).Info("Регистрируем расширение во фреше")

	if err := f.upLoadFile(filename); err == nil {
		url := f.Conf.SM.URL + f.Conf.SM.RegExtensionServiceURL + "?FileName=" + f.tempFile
		f.callServiceSM("GET", url, time.Minute)
	}

	logrus.WithField("файл", filename).Info("Расширение установлено")
}

func (f *Fresh) callServiceSM(method string, ServiceURL string, Timeout time.Duration) (result string) {
	logrus.Infof("Вызываем URL %v", ServiceURL)

	req, err := http.NewRequest(method, ServiceURL, nil)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	req.SetBasicAuth(f.Conf.SM.Login, f.Conf.SM.Pass)
	if f.ConfComment != "" {
		req.Header.Add("Msg", f.ConfComment)
	}
	if f.fileSize > 0 {
		req.Header.Add("Size", fmt.Sprintf("%d", f.fileSize))
	}

	client := &http.Client{Timeout: Timeout}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	if resp != nil {
		if err, result = f.readResp(resp); err != nil {
			panic(err) // выше по колстеку есть перехват
		}
	}
	return
}

func (f *Fresh) sendByte(b []byte) error {
	logrus.Debugf("Отправляем %v байт", len(b))

	/* requestBody := new(bytes.Buffer)
	multiPartWriter := multipart.NewWriter(requestBody)
	mPW, _ := multiPartWriter.CreateFormField("byte")
	mPW.Write(b)
	multiPartWriter.Close() */

	//req, err := http.NewRequest("PUT", f.Conf.UpLoadFileServiceURL, requestBody)
	url := f.Conf.SM.URL + f.Conf.SM.UpLoadFileServiceURL
	req, err := http.NewRequest("PUT", url, bytes.NewReader(b))
	if err != nil {
		logrus.WithField("Сервис", url).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		return err
	}
	req.SetBasicAuth(f.Conf.SM.Login, f.Conf.SM.Pass)

	//req.Header.Set("Content-Type", multiPartWriter.FormDataContentType())
	req.Header.Add("TempFile", f.tempFile)

	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("Сервис", url).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		return err
	}

	f.tempFile = resp.Header.Get("TempFile")
	err, _ = f.readResp(resp)
	return err
}

func (f *Fresh) readResp(resp *http.Response) (error, string) {
	defer resp.Body.Close()

	location, _ := resp.Location()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.WithField("location", location).Errorf("Произошла ошибка при чтении Body: %v", err)
		return err, ""
	}
	if !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed) {
		logrus.WithField("body", string(body)).WithField("location", location).Errorf("Код ответа %v", resp.StatusCode)
		return fmt.Errorf("Код возврата %v", resp.StatusCode), ""
	}
	return nil, string(body)
}

func (f *Fresh) GetListUpdateState(DateString string) (result string) {
	ServiceURL := f.Conf.SA.URL + f.Conf.SA.GetListUpdateState + "?Date=" + DateString
	logrus.Infof("Вызываем URL %v", ServiceURL)

	req, err := http.NewRequest("GET", ServiceURL, nil)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	req.SetBasicAuth(f.Conf.SA.Login, f.Conf.SA.Pass)
	client := &http.Client{Timeout: time.Second * 10}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("Сервис", ServiceURL).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при выполнении запроса: %v", err))
	}
	if resp != nil {
		if err, result = f.readResp(resp); err != nil {
			panic(err) // выше по колстеку есть перехват
		}
	}
	return
}

func (f *Fresh) GetAvailableUpdates(UUIDBase string) string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetAvailableUpdates + "?Base=" + UUIDBase
	return f.callServiceSM("GET", ServiceURL, time.Second*10)
}

func (f *Fresh) GetDatabase() string {
	ServiceURL := f.Conf.SM.URL + f.Conf.SM.GetDatabase
	return f.callServiceSM("GET", ServiceURL, time.Second*10)
}

func (f *Fresh) SetUpdetes(UUID string, UUIDBase string, MinuteShift int, funcDefer func()) (err error) {
	if funcDefer != nil {
		defer funcDefer()
	}
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("Произошла ошибка при вызове SetUpdetes: %v", e)
		}
	}()

	//start := time.Now().Add(time.Minute * time.Duration(MinuteShift))
	//start.Format("20060102230000")

	ServiceURL := f.Conf.SM.URL + f.Conf.SM.SetUpdetes + fmt.Sprintf("?UpdateUUID=%v&MinuteShift=%v&Base=%v", UUID, MinuteShift, UUIDBase)
	f.callServiceSM("PUT", ServiceURL, time.Minute)

	return nil
}
