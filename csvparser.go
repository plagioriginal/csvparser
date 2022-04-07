package csvparser

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// ParserFunc is the callback that will be called at each column parsing/reading
// The value parameter is the column value, and the destination is the struct to add values from the parsing
type ParserFunc[ReadTo any] func(value string, destination *ReadTo) error

// AfterParsingRowFunc is a callback/hook that will run after each row is parsed.
type AfterParsingRowFunc[ReadTo any] func(parsedObject ReadTo)

// csvParser is the internal object that will keep all the references needed to parse the file
type csvParser[ReadTo any] struct {
	fileReader       *csv.Reader
	columnParsers    map[string]ParserFunc[ReadTo]
	afterParsingHook AfterParsingRowFunc[ReadTo]
	headers          []string
}

// NewCsvParserFromBytes instantiates a new csvParser from a []byte input
// The *headers parameter are necessary if your .csv file doesn't contain headers
// by default. Adding headers to the constructor will make the parser know what to handle.
func NewCsvParserFromBytes[ReadTo any](input []byte, headers ...string) *csvParser[ReadTo] {
	return &csvParser[ReadTo]{
		fileReader:    csv.NewReader(bytes.NewReader(input)),
		headers:       headers,
		columnParsers: map[string]ParserFunc[ReadTo]{},
	}
}

// NewCsvParserFromReader instantiates a new csvParser from an io.Reader directly.
// Useful when reading from multipart files.
// The *headers parameter are necessary if your .csv file doesn't contain headers
// by default. Adding headers to the constructor will make the parser know what to handle.
func NewCsvParserFromReader[ReadTo any](input io.Reader, headers ...string) *csvParser[ReadTo] {
	return &csvParser[ReadTo]{
		fileReader:    csv.NewReader(input),
		headers:       headers,
		columnParsers: map[string]ParserFunc[ReadTo]{},
	}
}

// WithHook adds a handler that will run after every single parsing
func (c *csvParser[ReadTo]) WithHook(handler AfterParsingRowFunc[ReadTo]) {
	c.afterParsingHook = handler
}

// AddColumnParser adds a parser for each column to the internal parser list
func (c *csvParser[ReadTo]) AddColumnParser(headerName string, parser ParserFunc[ReadTo]) {
	c.columnParsers[headerName] = parser
}

// Parse returns an array of the object to return ([]ReadTo) from the input data and parsers provided.
func (c *csvParser[ReadTo]) Parse() ([]ReadTo, error) {
	err := c.prepareHeaders()
	if err != nil {
		return []ReadTo{}, err
	}
	return c.parseResults()
}

// prepareHeaders verifies if the headers and parsers are matched. If the headers are not passed in the constructor,
// it will load the headers from the file data.
func (c *csvParser[ReadTo]) prepareHeaders() error {
	if c.areHeadersEmpty() {
		return c.loadHeadersFromFile()
	}
	header, existsUnparsableHeader := c.isThereAnUnparsableHeader()
	if existsUnparsableHeader {
		return newUnparsableHeaderErr(header)
	}
	return nil
}

// areHeadersEmpty checks if the headers are empty
func (c *csvParser[ReadTo]) areHeadersEmpty() bool {
	return len(c.headers) == 0
}

// areHeadersAndParsersMatched makes sure that each header has an equivalent parser.
func (c *csvParser[ReadTo]) isThereAnUnparsableHeader() (string, bool) {
	for _, header := range c.headers {
		if !c.existsParserForHeader(header) {
			return header, true
		}
	}
	return "", false
}

// existsParserForHeader verifies if there is a parser defined for a specific header
func (c *csvParser[ReadTo]) existsParserForHeader(header string) bool {
	_, ok := c.getParserFor(header)
	return ok
}

// loadHeadersFromFile reads the first row in the file and loads it into the headers
func (c *csvParser[ReadTo]) loadHeadersFromFile() error {
	headers, err := c.fileReader.Read()
	if err != nil {
		return ParseError{Msg: fmt.Sprintf("couldn't read headers from file: %s", err.Error())}
	}
	return c.loadHeaders(headers)
}

// loadHeaders loads a set of headers into the struct.
func (c *csvParser[ReadTo]) loadHeaders(headers []string) error {
	for _, header := range headers {
		if err := c.loadHeader(header); err != nil {
			return err
		}
	}
	return nil
}

// loadHeader loads one header into the struct. If it is not able to be parsed
// (doesn't have an associated parser), it will return an error.
func (c *csvParser[ReadTo]) loadHeader(header string) error {
	header = strings.Trim(header, " ")
	if !c.isHeaderAbleToBeParsed(header) {
		return newUnparsableHeaderErr(header)
	}
	c.headers = append(c.headers, header)
	return nil
}

// isHeaderAbleToBeParsed verifies if there is a correspondent parser for said header.
func (c *csvParser[ReadTo]) isHeaderAbleToBeParsed(header string) bool {
	_, ok := c.getParserFor(header)
	return ok
}

// getParserFor gets a parser for a specific header.
func (c *csvParser[ReadTo]) getParserFor(header string) (ParserFunc[ReadTo], bool) {
	res, ok := c.columnParsers[header]
	return res, ok
}

// parseResults returns the slice of objects to be parsed from the .csv file.
func (c *csvParser[ReadTo]) parseResults() ([]ReadTo, error) {
	result := make([]ReadTo, 0)
	for {
		object, err := c.readRowAndParseObject()
		if err == io.EOF {
			break
		}
		if err != nil {
			return []ReadTo{}, newParseError(err)
		}
		result = append(result, *object)
	}
	return result, nil
}

// readRowAndParseObject reads a file row and parses it into an object.
func (c *csvParser[ReadTo]) readRowAndParseObject() (*ReadTo, error) {
	row, err := c.fileReader.Read()
	if err != nil {
		return nil, err
	}
	return c.parseRow(row)
}

// parseRow parses a single row into the target object. Runs the hook for the object if success.
func (c *csvParser[ReadTo]) parseRow(row []string) (*ReadTo, error) {
	object := new(ReadTo)
	err := c.parseColumns(row, object)
	if err != nil {
		return nil, err
	}
	c.runAfterParsingHook(object)
	return object, nil
}

// runHook runs the hook that is set up in the struct
func (c *csvParser[ReadTo]) runAfterParsingHook(object *ReadTo) {
	if c.afterParsingHookExists() {
		c.afterParsingHook(*object)
	}
}

func (c *csvParser[ReadTo]) afterParsingHookExists() bool {
	return c.afterParsingHook != nil
}

// parseColumns parses all the columns into a destination object.
func (c *csvParser[ReadTo]) parseColumns(row []string, destination *ReadTo) error {
	for i, columnValue := range row {
		err := c.parseColumn(columnValue, c.headers[i], destination)
		if err != nil {
			return err
		}
	}
	return nil
}

// parseColumn parses a single column. Uses columnParsers from the columnHeader to do it.
func (c *csvParser[ReadTo]) parseColumn(columnValue, columnHeader string, destination *ReadTo) error {
	parser, ok := c.getParserFor(columnHeader)
	if !ok {
		return newUnparsableHeaderErr(columnHeader)
	}
	if err := parser(columnValue, destination); err != nil {
		return err
	}
	return nil
}