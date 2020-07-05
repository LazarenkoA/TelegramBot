package netutility

import (
	//tel "1C/TelegramTasks"
	. "TelegramBot/Configuration"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/net/proxy"
)

type NetUtility struct {
	url, login, pass string
	Header           map[string]string
	Conf             *CommonConf
	Body io.Reader
}

func (net *NetUtility) Construct(url, login, pass string) *NetUtility {
	net.Header = make(map[string]string)

	net.url = url
	net.login = login
	net.pass = pass

	return net
}

func (net *NetUtility) DownloadFile(filepath string) error {
	logrus.Debugf("Загружаем файл %q", filepath)

	resp, err := GetHttpClient(net.Conf).Get(net.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func GetHttpClient(conf *CommonConf) *http.Client {
	// create a socks5 dialer
	httpClient := new(http.Client)
	if net_ := conf.Network; net_ != nil {
		logrus.Debug("Используем прокси " + net_.PROXY_ADDR)

		// setup a http client
		httpTransport := &http.Transport{}
		httpTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, nil
			default:
			}

			dialer, err := proxy.SOCKS5("tcp", net_.PROXY_ADDR, nil, proxy.Direct)
			if err != nil {
				logrus.WithField("Прокси", net_.PROXY_ADDR).Errorf("Ошибка соединения с прокси: %q", err)
				return nil, err
			}

			return dialer.Dial(network, addr)
		}
		httpClient = &http.Client{Transport: httpTransport}
	}

	return httpClient
}

func (net *NetUtility) CallHTTP(method string, Timeout time.Duration, beforeSend func(*http.Response)) (result string, err error) {
	logrus.WithField("method", method).WithField("Timeout", Timeout).Infof("Вызываем URL %v", net.url)

	req, err := http.NewRequest(method, net.url, net.Body)
	if err != nil {
		logrus.WithField("Сервис", net.url).Errorf("Произошла ошибка при регистрации запроса: %v", err)
		panic(fmt.Errorf("Произошла ошибка при загрузки файла: %v", err))
	}
	if net.login != "" {
		req.SetBasicAuth(net.login, net.pass)
	}

	for k, v := range net.Header {
		req.Header.Add(k, v)
	}

	client := &http.Client{Timeout: Timeout}
	resp, err := client.Do(req)
	if err != nil {
		logrus.WithField("URL", net.url).Errorf("Произошла ошибка при выполнении запроса: %v", err)
		return "", err
	}
	if resp != nil {
		if err, result = net.readResp(resp); err != nil {
			return "", err
		}

		if beforeSend != nil {
			beforeSend(resp)
		}
	}
	return result, nil
}

func (net *NetUtility) SendByte(method string, b []byte, beforeSend func(*http.Response)) error {
	logrus.WithField("URL", net.url).Debugf("Отправляем %v байт", len(b))
	defer func() {
		net.Body = nil
	}()

	net.Body = bytes.NewReader(b)
	_, err := net.CallHTTP(method, time.Minute *5, beforeSend)
	return err
}

func (net *NetUtility) readResp(resp *http.Response) (error, string) {
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.WithField("URL", resp.Request.URL).Errorf("Произошла ошибка при чтении Body: %v", err)
		return err, ""
	}
	if !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed) {
		logrus.WithField("body", string(body)).WithField("URL", resp.Request.URL).Errorf("Код ответа %v", resp.StatusCode)
		return fmt.Errorf("Код возврата %v", resp.StatusCode), ""
	}
	return nil, string(body)
}
