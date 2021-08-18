# ds-audiorepo

Описывает работу с аудио-репозиторием. Обмен сообщениями с микросервисом реализован с использованием [RabbitMQ](https://www.rabbitmq.com).

Микросервис работает одновременно в двух режимах: автономном и командном.

В автономном режиме микросервис отправляет подписчикам изменения в систаве каталогов альбомов (Album Entry): создание, переименование и удаление каталогов.

В командном режиме микросервис отвечает за приведение каталогов альбомов к единому инфраструктурному порядку:
- формат наименования каталога альбома
- формат наименования подкаталога графического материала по релизу (сканы)
- формат наименования файлов треков
- наименование файла обложки релиза
- очистка каталога от технических данных и прочих файлов


Команды микросервиса:
---
| Команда |                            Назначение                                |
|---------|----------------------------------------------------------------------|
|normalize|-- пока не реализована --                                             |
|ping     |проверка жизнеспособности микросервиса                                |

Пример запуска микросервиса:
---
```go
    package main

    import (
	    "flag"
	    "fmt"

	    log "github.com/sirupsen/logrus"

	    repokeeper "github.com/ytsiuryn/ds-audiorepo"
	    srv "github.com/ytsiuryn/ds-microservice"
    )

    func main() {
	    connstr := flag.String(
		    "msg-server",
		    "amqp://guest:guest@localhost:5672/",
		    "Message server connection string")

		product := flag.Bool(
			"product",
			false,
			"product-режим запуска сервиса")

		flag.Parse()

	    log.Info(fmt.Sprintf("%s starting..", repokeeper.ServiceName))

	    keeper := repokeeper.New()

		msgs := keeper.ConnectToMessageBroker(*connstr)

		if *product {
			keeper.Log.SetLevel(log.InfoLevel)
		} else {
			keeper.Log.SetLevel(log.DebugLevel)
		}

		keeper.Start(msgs)
	}
```
