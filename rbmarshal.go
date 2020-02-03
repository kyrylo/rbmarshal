// Implemented according to the spec:
// https://github.com/ruby/ruby/blob/b01c28eeb3942bce1ddf9b9243ecf727d5421c6d/doc/marshal.rdoc
package rbmarshal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
)

// Marshaled data has major and minor version numbers stored along with
// the object information (first two bytes).
var marshalVersion = [2]byte{0x04, 0x08}

// Special byte sequences used to denote encoding lengths of a string. Ugly name
// but encodings in marshal.c are mysterious.
var (
	fiveDigitEnc = [2]byte{0x06, 0x3A}
	fourDigitEnc = [2]byte{0x06, 0x3B}
)

const (
	// These objects are each one byte long.

	typeNil   = '0'
	typeTrue  = 'T'
	typeFalse = 'F'

	// A signed 32 bit value.
	typeFixnum = 'i'

	// If the fixnum is positive, the value is determined by subtracting the
	// offest from the value. If the fixnum is negative, the value is
	// determined by adding the offest to the value.
	fixnumOffset = 5

	// typeExtended   = 'e'
	// typeUclass     = 'C'
	// typeObject     = 'o'
	// typeData       = 'd'
	// typeUserdef    = 'u'
	// typeUsrmarshal = 'U'
	typeFloat    = 'f'
	typeBignum   = 'l'
	bignumPos    = '+'
	bignumNeg    = '-'
	bignumOffset = 10 // not sure why 10 but it does the job

	typeString = '"'
	typeRegexp = '/'
	typeArray  = '['
	typeHash   = '{'
	// typeHashDef    = '}'
	// typeStruct     = 'S'
	// typeModuleOld  = 'M'
	// typeClass      = 'c'
	// typeModule     = 'm'

	typeSymbol  = ':'
	typeSymlink = ';'

	typeIvar = 'I'
	// typeLink = '@'
)

const (
	encStart  = 0x06
	semicolon = 0x3b
	colon     = 0x3a
)

type LoadArg struct {
	Symbols []string
}

func Load(r *bufio.Reader) (interface{}, error) {
	if err := validateVersion(r); err != nil {
		return nil, err
	}

	return read(r, new(LoadArg))
}

func validateVersion(r *bufio.Reader) error {
	var version [2]byte
	_, err := io.ReadFull(r, version[:])
	if err != nil {
		return err
	}

	if version != marshalVersion {
		return fmt.Errorf(
			"unsupported marshal version %v, wanted %v",
			version, marshalVersion,
		)
	}

	return nil
}

func read(r *bufio.Reader, arg *LoadArg) (interface{}, error) {
	byte, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch byte {
	case typeNil:
		return nil, nil
	case typeTrue:
		return true, nil
	case typeFalse:
		return false, nil
	case typeFixnum:
		return readFixnum(r, arg)
	case typeBignum:
		return readBignum(r, arg)
	case typeString:
		return readString(r, arg)
	case typeArray:
		return readArray(r, arg)
	case typeFloat:
		return readFloat(r, arg)
	case typeIvar:
		return readIvar(r, arg)
	case typeRegexp:
		return readRegexp(r, arg)
	case typeSymbol:
		return readSymbol(r, arg)
	case typeSymlink:
		return readSymlink(r, arg)
	case typeHash:
		return readHash(r, arg)
	default:
		fmt.Printf("unsupported type byte: %v\n", byte)
	}

	return nil, nil
}

func readFixnum(r *bufio.Reader, arg *LoadArg) (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	c := int(int8(b))

	if c == 0 {
		return 0, nil
	}

	if c > 0 {
		if 4 < c && c < 128 {
			return c - 5, nil
		}

		n := 0
		for i := 0; i < c; i++ {
			b, err = r.ReadByte()
			if err != nil {
				return 0, err
			}
			n |= int(b) << (8 * i)
		}
		return n, nil
	} else {
		if -129 < c && c < -4 {
			return c + 5, nil
		}

		c = -c
		n := -1
		for i := 0; i < c; i++ {
			n &= ^(0xFF << (8 * i))
			b, err = r.ReadByte()
			if err != nil {
				return 0, err
			}
			n |= int(b) << (8 * i)
		}
		return n, nil
	}

	return 0, nil
}

func readBignum(r *bufio.Reader, arg *LoadArg) (int, error) {
	sign, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	rawLen, err := r.ReadByte()
	if err != nil {
		return 0, err
	}

	len := int(2*rawLen - bignumOffset)
	data := make([]byte, len)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return 0, err
	}

	n := 0
	for i := 0; i < len; i++ {
		n |= int(data[i]) << (8 * i)
	}

	switch sign {
	case bignumPos:
		return n, nil
	case bignumNeg:
		return -n, nil
	default:
		panic("unexpected sign")
	}
}

func readIvar(r *bufio.Reader, arg *LoadArg) (interface{}, error) {
	bytes, err := r.Peek(1)
	if err != nil {
		return "", err
	}

	b := bytes[0]
	switch b {
	case typeString:
		return readString(r, arg)
	default:
		return read(r, arg)
	}
}

func readString(r *bufio.Reader, arg *LoadArg) (string, error) {
	bytes, err := r.Peek(1)
	if err != nil {
		return "", err
	}

	b := bytes[0]
	if b != typeString {
		return readBinaryString(r, arg)
	}

	// Skip the typeString byte.
	_, err = r.ReadByte()
	if err != nil {
		return "", err
	}
	return readEncodedString(r, arg)
}

func readBinaryString(r *bufio.Reader, arg *LoadArg) (string, error) {
	len, err := readFixnum(r, arg)
	if err != nil {
		return "", err
	}

	str := make([]byte, len)
	_, err = io.ReadFull(r, str)
	if err != nil {
		return "", err
	}

	return string(str), nil
}

func readEncodedString(r *bufio.Reader, arg *LoadArg) (string, error) {
	len, err := readFixnum(r, arg)
	if err != nil {
		return "", err
	}

	str := make([]byte, len)
	_, err = io.ReadFull(r, str)
	if err != nil {
		return "", err
	}

	if err = stripEncoding(r, arg); err != nil {
		return "", err
	}

	return string(str), nil
}

// Encoding is not used anywhere at the moment, so we just move the pointer
// forwards.
func stripEncoding(r *bufio.Reader, arg *LoadArg) error {
	var signature [2]byte
	_, err := io.ReadFull(r, signature[:])
	if err != nil {
		return err
	}

	var len int // how many more bytes to strip
	if signature == fiveDigitEnc {
		len = 3
	} else if signature == fourDigitEnc {
		len = 2
	} else {
		return errors.New(
			fmt.Sprintf(
				"unsupported string encoding signature %v",
				signature,
			),
		)
	}

	enc := make([]byte, len)
	_, err = io.ReadFull(r, enc)
	if err != nil {
		return err
	}

	return nil
}

func readArray(r *bufio.Reader, arg *LoadArg) ([]interface{}, error) {
	size, err := readFixnum(r, arg)
	if err != nil {
		return make([]interface{}, 0), err
	}

	arr := make([]interface{}, size)
	for i := 0; i < size; i++ {
		arr[i], err = read(r, arg)
		if err != nil {
			return arr, err
		}
	}

	return arr, nil
}

func readFloat(r *bufio.Reader, arg *LoadArg) (float64, error) {
	str, err := readString(r, arg)
	if err != nil {
		return 0, err
	}

	switch str {
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	default:
		f, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return 0, err
		}

		return f, nil
	}
}

func readRegexp(r *bufio.Reader, arg *LoadArg) (*regexp.Regexp, error) {
	str, err := readString(r, arg)
	if err != nil {
		return regexp.MustCompile(""), err
	}

	options, err := r.ReadByte()
	if err != nil {
		return regexp.MustCompile(""), err
	}

	switch options {
	case 0: // o - perform #{} interpolation only once
		// doesn't make sense in Go
	// i (1) - case-insensitive
	// ix (3)
	case 1, 3:
		str = "(?i)" + str
	case 2: // x - ignore whitespace and comments
		// unsupported by Go
	// m (4) - treat a newline as a character matched by .
	// xm (6)
	case 4, 6:
		str = "(?s)" + str
	case 5, 7: // (5) - im, (7) - xmi
		str = "(?is)" + str
	default:
		// Other cases invlove Regexp encoding, which we don't care
		// about at the moment.
	}

	bytes, err := r.Peek(2)
	if err != nil {
		return regexp.MustCompile(""), err
	}
	if bytes[0] == encStart && (bytes[1] == colon || bytes[1] == semicolon) {
		stripEncoding(r, arg)
	}

	return regexp.Compile(str)
}

// Returns strings for now but if this library will ever support encoding, we
// will need a proper solution, so that we don't dump strings when they should
// be symbols.
func readSymbol(r *bufio.Reader, arg *LoadArg) (string, error) {
	s, err := readString(r, arg)
	if err != nil {
		return "", err
	}
	arg.Symbols = append(arg.Symbols, s)
	return s, nil
}

func readSymlink(r *bufio.Reader, arg *LoadArg) (string, error) {
	i, err := readFixnum(r, arg)
	if err != nil {
		return "", err
	}
	return arg.Symbols[i], nil
}

func readHash(r *bufio.Reader, arg *LoadArg) (map[string]interface{}, error) {
	size, err := readFixnum(r, arg)
	if err != nil {
		return map[string]interface{}{}, err
	}

	hash := make(map[string]interface{}, size)
	for i := 0; i < size; i++ {
		key, err := read(r, arg)
		if err != nil {
			return hash, err
		}
		val, err := read(r, arg)
		if err != nil {
			return hash, err
		}

		var k string
		switch key := key.(type) {
		case string:
			k = key
		case int:
			k = strconv.Itoa(key)
		default:
			k = ""
		}
		hash[k] = val
	}

	return hash, nil
}
