// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

// Package catalog provides helpers for using the Winlink 2000 catalog services.
package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/la5nta/wl2k-go/fbb"
)

type PosReport struct {
	Date     time.Time
	Lat, Lon *float64 // In decimal degrees
	Speed    *float64 // Unit not specified in winlink docs
	Course   *Course
	Comment  string // Up to 80 characters
}

type Course struct {
	Digits   [3]byte
	Magnetic bool
}

func NewCourse(degrees int, magnetic bool) (*Course, error) {
	if degrees < 0 || degrees > 360 {
		return nil, errors.New("degrees out of bounds [0,360]")
	}
	if degrees == 360 {
		degrees = 0
	}
	c := Course{Magnetic: magnetic}
	copy(c.Digits[:], []byte(fmt.Sprintf("%3d", degrees)))
	return &c, nil
}

func (c Course) String() string {
	if c.Magnetic {
		return fmt.Sprintf("%sM", string(c.Digits[:]))
	} else {
		return fmt.Sprintf("%sT", string(c.Digits[:]))
	}
}

func (p PosReport) Message(mycall string) *fbb.Message {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "DATE: %s\r\n", p.Date.UTC().Format(fbb.DateLayout))

	if p.Lat != nil && p.Lon != nil {
		fmt.Fprintf(&buf, "LATITUDE: %s\r\n", decToMinDec(*p.Lat, true))
		fmt.Fprintf(&buf, "LONGITUDE: %s\r\n", decToMinDec(*p.Lon, false))
	}
	if p.Speed != nil {
		fmt.Fprintf(&buf, "SPEED: %f\r\n", *p.Speed)
	}
	if p.Course != nil {
		fmt.Fprintf(&buf, "COURSE: %s\r\n", *p.Course)
	}
	if len(p.Comment) > 0 {
		fmt.Fprintf(&buf, "COMMENT: %s\r\n", p.Comment)
	}

	msg := fbb.NewMessage(fbb.PositionReport, mycall)

	err := msg.SetBody(buf.String())
	if err != nil {
		panic(err)
	}

	msg.SetSubject("POSITION REPORT")
	msg.AddTo("QTH")

	return msg
}

// Format: 23-42.3N
func decToMinDec(dec float64, latitude bool) string {
	var sign byte
	if latitude && dec > 0 {
		sign = 'N'
	} else if latitude && dec < 0 {
		sign = 'S'
	} else if !latitude && dec > 0 {
		sign = 'E'
	} else if !latitude && dec < 0 {
		sign = 'W'
	} else {
		sign = ' '
	}

	deg := int(dec)
	min := (dec - float64(deg)) * 60.0

	var format string
	if latitude {
		format = "%02.0f-%07.4f%c"
	} else {
		format = "%03.0f-%07.4f%c"
	}

	return fmt.Sprintf(format, math.Abs(float64(deg)), math.Abs(min), sign)
}
