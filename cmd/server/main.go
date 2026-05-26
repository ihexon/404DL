package main

import (
	"os"

	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if err := newApp().Run(os.Args); err != nil {
		logrus.WithError(err).Fatal("command failed")
	}
}
