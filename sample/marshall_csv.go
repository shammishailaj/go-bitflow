package sample

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	csv_separator_rune = ','
	csv_newline        = "\n"
	csv_separator      = string(csv_separator_rune)
	csv_date_format    = "2006-01-02 15:04:05.999999999"
)

type CsvMarshaller struct {
}

func (*CsvMarshaller) String() string {
	return "CSV"
}

func (*CsvMarshaller) WriteHeader(header Header, writer io.Writer) error {
	w := WriteCascade{Writer: writer}
	w.WriteStr(time_col)
	if header.HasTags {
		w.WriteStr(csv_separator)
		w.WriteStr(tags_col)
	}
	for _, name := range header.Fields {
		w.WriteStr(csv_separator)
		w.WriteStr(name)
	}
	w.WriteStr(csv_newline)
	return w.Err
}

func readCsvLine(reader *bufio.Reader) ([]string, bool, error) {
	line, err := reader.ReadString(csv_newline[0])
	eof := err == io.EOF
	if err != nil && !eof {
		return nil, false, err
	}
	if len(line) == 0 {
		return nil, eof, nil
	}
	line = line[:len(line)-1] // Strip newline char
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == csv_separator_rune
	}), eof, nil
}

func (*CsvMarshaller) ReadHeader(reader *bufio.Reader) (header Header, err error) {
	fields, eof, err := readCsvLine(reader)
	if err != nil {
		return
	}
	if len(fields) == 0 && eof {
		err = io.EOF
		return
	}
	if err = checkFirstCol(fields[0]); err != nil {
		return
	}
	header.HasTags = len(fields) >= 2 && fields[1] == tags_col
	start := 1
	if header.HasTags {
		start++
	}
	header.Fields = fields[start:]
	return
}

func (*CsvMarshaller) WriteSample(sample Sample, header Header, writer io.Writer) error {
	w := WriteCascade{Writer: writer}
	w.WriteStr(sample.Time.Format(csv_date_format))
	if header.HasTags {
		tags := sample.TagString()
		w.WriteStr(csv_separator)
		w.WriteStr(tags)
	}
	for _, value := range sample.Values {
		w.WriteStr(csv_separator)
		w.WriteStr(fmt.Sprintf("%v", value))
	}
	w.WriteStr(csv_newline)
	return w.Err
}

func (*CsvMarshaller) ReadSample(header Header, reader *bufio.Reader) (sample Sample, err error) {
	fields, eof, err := readCsvLine(reader)
	if err != nil {
		return
	}
	if len(fields) == 0 && eof {
		err = io.EOF
		return
	}
	sample.Time, err = time.Parse(csv_date_format, fields[0])
	if err != nil {
		return
	}

	start := 1
	if header.HasTags {
		if len(fields) < 2 {
			err = fmt.Errorf("Sample too short: %v", fields)
			return
		}
		if err = sample.ParseTagString(fields[1]); err != nil {
			return
		}
		start++
	}

	for _, field := range fields[start:] {
		var val float64
		if val, err = strconv.ParseFloat(field, 64); err != nil {
			return
		}
		sample.Values = append(sample.Values, Value(val))
	}
	return sample, nil
}
