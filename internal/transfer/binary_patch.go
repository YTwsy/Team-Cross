package transfer

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Git binary patches are compressed base85 data. Bounding JSON/patch bytes alone
// does not bound git apply's disk output, especially for delta instructions.
func validateBinaryPatchLimits(patch []byte) error {
	lines := bytes.Split(patch, []byte{'\n'})
	renameSources := map[string]bool{}
	for _, raw := range lines {
		line := string(raw)
		if strings.HasPrefix(line, "copy from ") || strings.HasPrefix(line, "copy to ") {
			return errors.New("offline patches do not support copy headers; export ordinary full file additions")
		}
		if strings.HasPrefix(line, "rename from ") {
			name := strings.TrimPrefix(line, "rename from ")
			if strings.HasPrefix(name, `"`) {
				value, err := strconv.Unquote(name)
				if err != nil {
					return errors.New("invalid quoted rename path")
				}
				name = value
			}
			if err := ValidatePath(name); err != nil {
				return err
			}
			key := portablePathKey(name)
			if renameSources[key] {
				return errors.New("repeated rename source would duplicate materialized content")
			}
			renameSources[key] = true
		}
		if strings.HasSuffix(line, " 160000") && (strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "old mode ") || strings.HasPrefix(line, "new mode ") || strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ")) {
			return errors.New("offline patches do not support Git submodule dependencies")
		}
	}
	totalResult := uint64(0)
	for index := 0; index < len(lines); index++ {
		if string(lines[index]) != "GIT binary patch" {
			continue
		}
		index++
		for block := 0; block < 2; block++ {
			if index >= len(lines) {
				return errors.New("incomplete Git binary patch")
			}
			header := strings.Fields(string(lines[index]))
			if len(header) != 2 || (header[0] != "literal" && header[0] != "delta") {
				return errors.New("invalid Git binary patch header")
			}
			size, err := strconv.ParseUint(header[1], 10, 64)
			if err != nil || size > MaxObjectBytes {
				return errors.New("binary patch payload exceeds 16 MiB object limit")
			}
			index++
			var compressed bytes.Buffer
			for index < len(lines) && len(lines[index]) > 0 {
				data, err := decodeGit85(lines[index])
				if err != nil {
					return err
				}
				if compressed.Len()+len(data) > MaxObjectBytes {
					return errors.New("compressed binary patch exceeds limit")
				}
				compressed.Write(data)
				index++
			}
			reader, err := zlib.NewReader(bytes.NewReader(compressed.Bytes()))
			if err != nil {
				return fmt.Errorf("invalid binary patch compression: %w", err)
			}
			data, readErr := io.ReadAll(io.LimitReader(reader, MaxObjectBytes+1))
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || uint64(len(data)) != size {
				return errors.New("binary patch decompression size mismatch")
			}
			resultSize := size
			if header[0] == "delta" {
				base, rest, err := readDeltaSize(data)
				if err != nil {
					return err
				}
				result, _, err := readDeltaSize(rest)
				if err != nil {
					return err
				}
				if base > MaxObjectBytes || result > MaxObjectBytes {
					return errors.New("binary delta expands beyond 16 MiB object limit")
				}
				resultSize = result
			}
			totalResult += resultSize
			if totalResult > MaxTotalBytes {
				return errors.New("binary patch expands beyond 64 MiB aggregate limit")
			}
			index++
			// Forward-only patches are valid Git input. Reverse data is optional.
			if index >= len(lines) || (!bytes.HasPrefix(lines[index], []byte("literal ")) && !bytes.HasPrefix(lines[index], []byte("delta "))) {
				index--
				break
			}
		}
	}
	return nil
}

const git85Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz!#$%&()*+-;<=>?@^_`{|}~"

func decodeGit85(line []byte) ([]byte, error) {
	if len(line) == 0 {
		return nil, errors.New("empty Git base85 line")
	}
	length := 0
	switch {
	case line[0] >= 'A' && line[0] <= 'Z':
		length = int(line[0]-'A') + 1
	case line[0] >= 'a' && line[0] <= 'z':
		length = int(line[0]-'a') + 27
	default:
		return nil, errors.New("invalid Git base85 line size")
	}
	if len(line) != 1+((length+3)/4)*5 {
		return nil, errors.New("invalid Git base85 line length")
	}
	decoded := make([]byte, 0, ((length+3)/4)*4)
	for index := 1; index < len(line); index += 5 {
		value := uint64(0)
		for offset := 0; offset < 5; offset++ {
			digit := strings.IndexByte(git85Alphabet, line[index+offset])
			if digit < 0 {
				return nil, errors.New("invalid Git base85 digit")
			}
			value = value*85 + uint64(digit)
		}
		if value > 0xffffffff {
			return nil, errors.New("Git base85 integer overflow")
		}
		decoded = append(decoded, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
	}
	return decoded[:length], nil
}

func readDeltaSize(data []byte) (uint64, []byte, error) {
	var result uint64
	for index := 0; index < len(data) && index < 10; index++ {
		if index == 9 && data[index] > 1 {
			return 0, nil, errors.New("binary delta size overflow")
		}
		result |= uint64(data[index]&0x7f) << uint(index*7)
		if data[index]&0x80 == 0 {
			return result, data[index+1:], nil
		}
	}
	return 0, nil, errors.New("invalid binary delta size")
}
