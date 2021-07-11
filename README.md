# ds-audiorepo

Описывает работу с аудио-репозиторием. Обмен сообщениями с микросервисом реализован с использованием [RabbitMQ](https://www.rabbitmq.com).

Поддержка аудиоформатов:
---
- mp3 (id3v1/id3v2)
- flac (id3v2/vorbis comments)
- dsf (id3v2)
- wavpack (id3v2/apev2; без аудиосвойств треков)

Команды микросервиса:
---
| Команда |                            Назначение                                |
|---------|----------------------------------------------------------------------|
|update   |формирование списка измененных каталогов альбомов с последней проверки|
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
