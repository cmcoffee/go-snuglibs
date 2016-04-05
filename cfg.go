/* Package 'cfg' provides functions for reading and writing configuration files and their coresponding string values.
   Ignores '#' as comment lines, ','s denote multiple values.

   # Example config file.
   [section]
   key = value
   key2 = value1, value2
   key3 = value1,
          value2,
          value3

   [section2]
   key = value1,
         value2,
         value3
*/
package cfg

import (
	"bufio"
	"fmt"
	"os"
	"bytes"
	"strings"
	"io/ioutil"
	"sync"
	"io"
)

type Store struct {
	file string
	mutex *sync.RWMutex
	cfgStore map[string]map[string][]string
}

const (
	cfg_HEADER = 1 << iota
	cfg_KEY
	cfg_COMMA
	cfg_ESCAPE
)

// Returns array of all retrieved string values under section with key.
func (s *Store) Get(section, key string) ([]string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	section = strings.ToLower(section)
	key = strings.ToLower(key)
	if result, found := s.cfgStore[section][key]; !found { 
		return []string{""}
	} else {
		if len(result) == 0 { return []string{""} }

		// Remove escape characters.
		for i, val := range result {
			result[i] = strings.Replace(val, "\\", "", -1)
			fmt.Println(result[i])
		}
		return result
	}
}


// Returns array of all sections in config file.
func (s *Store) ListSections() (out []string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	for section, _ := range s.cfgStore {
		out = append(out, section)
	}
	return
}

// Returns keys of section specified.
func (s *Store) ListKeys(section string) (out []string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if v, ok := s.cfgStore[section]; !ok { 
		return nil 
	} else {
		for key, _ := range v { 
			out = append(out, key)
		}
	}
	return
}

// Returns true if section or section and key exists.
func (s *Store) Exists(input...string) (found bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	inlen := len(input)
	if inlen == 0 { return false }
	if inlen > 0 {
		if _, found = s.cfgStore[input[0]]; found { return } 
	}
	if inlen > 1 {
		if found == true {
			_, found = s.cfgStore[input[0]][input[1]]
			return
		}
	}
	return
}

// Sets key = values under [section], updates Store and saves to file.
func (s *Store) Set(section, key string, value...string) (err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	section = strings.ToLower(section)
	key = strings.ToLower(key)
	var newValue []string
	for _, val := range value { newValue = append(newValue, val) }

	if err := SetFile(s.file, section, key, newValue[0:]...); err != nil { return err }

	// Create new map if one doesn't exist.
	if _, ok := s.cfgStore[section]; !ok {
		s.cfgStore[section] = make(map[string][]string)
	}

	s.cfgStore[section][key] = newValue
	return
}

func setKey(buf *bytes.Buffer) (key string) {
	key = strings.ToLower(strings.TrimSpace(buf.String()))
	buf.Reset()
	return
}

func addVal(buf *bytes.Buffer, val *[]string) {
	*val = append(*val, strings.TrimSpace(buf.String()))
	buf.Reset()
}

func cfgErr(file string, line int) error { return fmt.Errorf("Syntax error found in %s on line %d.", file, line) }

// Creates a new empty config file & Store, overwriting an existing file with comments if specified.
func Create(file string, comment...string) (out *Store, err error) {
	f, err := os.Create(file)
	if err != nil { return nil, err }
	defer f.Close()
	out = &Store{
		file,
		new(sync.RWMutex),
		make(map[string]map[string][]string),
	}
	if len(comment) > 0 {
		for _, c := range comment {
			f.WriteString("# " + c + "\n");
		}
	}
	return
}

// Reads configuration file and returns Store.
func Load(file string) (out *Store, err error) {
	f, err := os.Open(file)
	if err != nil { return nil, err }
	defer f.Close()
	s := bufio.NewScanner(f)
	
	var flag, line, last int
	
	buf := &bytes.Buffer{}
	var section, key string
	var val []string
	out = &Store{
		file,
		new(sync.RWMutex),
		make(map[string]map[string][]string),
	}
	
	scanLoop:
	for s.Scan() {
		line++
		txt := s.Text() + "\n"
		l := len(txt)
		if l < 2 { continue }
		
		for i, ch := range txt {
			if (flag & cfg_ESCAPE) > 0 {
				if i == l - 1 && buf.Len() != 0 {
					return out, cfgErr(file, line)
				}
				buf.WriteRune(ch)
				flag &^= cfg_ESCAPE
				continue
			}
			switch ch {
			case '[':
				if (flag & cfg_KEY != 0) { return out, cfgErr(file, last) }
				last = line
				if l > 2 && strings.ContainsAny(txt, "[ & ]") {
					section = txt[1:l-2]
					flag |= cfg_HEADER
					section = strings.ToLower(section)
					out.cfgStore[section] = make(map[string][]string)
					continue scanLoop
				} else { return out, cfgErr(file, line) }
			case '#': 
				continue scanLoop;
			case '=':
				if (flag & cfg_KEY != 0) { return out, cfgErr(file, line) }
				flag |= cfg_KEY
				key = setKey(buf)
				last = line
			case ',':
				if (flag & cfg_KEY == 0) { return out, cfgErr(file, line) }
				addVal(buf, &val)
				last = line
				flag |= cfg_COMMA
			case '\n':
				if (flag & cfg_KEY == 0) { return out, cfgErr(file, line) }
				if (flag & cfg_COMMA != 0) {
					flag &^= cfg_COMMA
					continue
				}
				flag &^= cfg_HEADER
				flag &^= cfg_KEY
				addVal(buf, &val)
				out.cfgStore[section][key] = val
				val = nil
				last = line
				continue scanLoop
			case '\\':
				flag |= cfg_ESCAPE
				fallthrough
			default:
				if buf.Len() == 0 {
					switch ch {
						case ' ':
							fallthrough
						case '\t':
							continue
					}
				}
				flag &^= cfg_COMMA
				if i == l - 1 && buf.Len() != 0 {
					return out, cfgErr(file, line)
				}
				buf.WriteRune(ch)
			}
				
		}
	}
	if (flag & cfg_KEY != 0) {
		return out, cfgErr(file, last)
	}
	return out, nil	
}

// Returns map of specific [section] within configuration file.
func ReadFile(file, section string) (out map[string][]string, err error) {
	section = strings.ToLower(section)
	f, err := os.Open(file)
	if err != nil { return nil, err }
	defer f.Close()
	s := bufio.NewScanner(f)
	
	var flag, line, last int
	
	buf := &bytes.Buffer{}
	var key string
	var val []string
	out = make(map[string][]string)
		
	for s.Scan() {
		line++
		txt := s.Text() + "\n"
		l := len(txt)
		
		if l > 1 && txt[0] == '#' || l == 1 { continue }
		
		if (flag & cfg_HEADER == 0) {
	
			// Skip to section sections only.
			if l > 1 { 
				if !strings.ContainsAny(txt, "[ & ]") { continue }
			} else { continue }
			
			txt = strings.ToLower(txt)
			
			if (strings.HasPrefix(txt, "[" + section + "]")) {
				flag |= cfg_HEADER 
			}
			
		} else {
			for i, ch := range txt {
				if (flag & cfg_ESCAPE) > 0 {
					if i == l - 1 && buf.Len() != 0 {
						return out, cfgErr(file, line)
					}	
					buf.WriteRune(ch)
					flag &^= cfg_ESCAPE
					continue
				}
				switch ch {
				case '=':
					if (flag & cfg_KEY != 0) { return out, cfgErr(file, line) }
					flag |= cfg_KEY
					key = setKey(buf)
					last = line
				case ',':
					if (flag & cfg_KEY == 0) { return out, cfgErr(file, line) }
					addVal(buf, &val)
					last = line
					flag |= cfg_COMMA
				case '\n':
					if (flag & cfg_KEY == 0) { return out, cfgErr(file, line) }
					if (flag & cfg_COMMA > 0) {
						flag &^= cfg_COMMA
						continue
					}
					flag &^= cfg_KEY
					addVal(buf, &val)
					out[key] = val
					val = nil
					last = line
				case '[':
					if (flag & cfg_KEY != 0) { return out, cfgErr(file, last) }
					return
				case '\\':
					flag |= cfg_ESCAPE
					fallthrough
				default:
					if buf.Len() == 0 {
						switch ch {
							case ' ':
								fallthrough
							case '\t':
								continue
					 	}
					}
					flag &^= cfg_COMMA
					if i == l - 1 && buf.Len() != 0 {
						return out, cfgErr(file, line)
					}
					buf.WriteRune(ch)
				}
				
			}
		}
	}
	return out, nil
}

// Writes key = values under [section] to File.
func SetFile(file, section, key string, value...string) error {
	for _, val := range value {
		for _, ch := range val {
			switch ch {
				case '[':
					fallthrough
				case ']':
					fallthrough
				case ',':
					return fmt.Errorf("Invalid character found in value: '%c' found in \"%s\".", ch, val )
			}
		}
	}
	
	section = strings.ToLower(section)
	key = strings.ToLower(key)
	f, err := os.Open(file)
	if err != nil { return err }
	defer f.Close()
	
	// Generate temp file, then close it, reopen it with append.
	tmp, err := ioutil.TempFile(os.TempDir(), fmt.Sprintf("%s.temp_conf.", os.Args[0]))
	if err != nil { return err }
	tmpfname := tmp.Name()
	
	no_end_comma := func(input string) (no_comma bool) {
		no_comma = true
		for _, ch := range input {
			switch ch {
				case ',':
					no_comma = false
				case '\t':
					fallthrough
				case ' ':
					fallthrough
				default:
					no_comma = true
			}
		}
		return
	}
	
	// cfgSeek returns first half and bottom half of file, excluding the key = value.
	cfgSeek := func(section, key string, f *os.File) (upper int, lower int, flag int) {
		f.Seek(0,0)
		s := bufio.NewScanner(f)
		
		var line int
	
		for s.Scan() {
			line++
			b := s.Text()
			b = strings.ToLower(b)
			l := len(b)
		
			if l > 0 && b[0] == '#' || l == 0 { continue }
		
			if (flag & cfg_HEADER == 0) {
				if strings.HasPrefix(b, "[" + section + "]") { 
					flag |= cfg_HEADER 
					upper = line
					continue
				}
			} else {
				// if we hit the next [section], we didn't find the key to replace, which means its new.
				if b[0] == '[' {
					lower = upper + 1
					return
				}
			}

			if (flag & cfg_HEADER > 0) {
				if (flag & cfg_KEY == 0) && strings.HasPrefix(b, key) {
					pfx := strings.Split(b, "=")
					if strings.TrimSpace(pfx[0]) == key { 
						upper = line - 1
						flag |= cfg_KEY 
					}
				}
				if (flag & cfg_KEY > 0) && no_end_comma(b) {
					lower = line + 1
					return
				 }
			}
		}
		return line, -1, flag
	}
	
	head, tail, flag := cfgSeek(section, key, f)
	
	// Copys line start to line end of src file to dst file.
	copyFile := func(src, dst *os.File, start, end int) error {
		_, err := src.Seek(0, 0)
		if err != nil { return err }
		
		s := bufio.NewScanner(src)
		var line int
		
		for line < start {
			s.Scan()
			line++
		}

		for (line < end || end == -1) && s.Scan() {
			line++
			_, err := dst.WriteString(s.Text() + "\n")
			if err != nil { return err }
		}
		return nil
	}

	var txt []string
	
	if (flag & cfg_HEADER == 0) { txt = append(txt, "[" + section + "]") }
	
	var spacer []byte
	
	for i, str := range value {
		if str == "" { break }
		if i == 0 {
			txt = append(txt, key + " = " + str)
			spacer = make([]byte, len(key + " = "))
			for ch := range spacer {
				spacer[ch] = ' '
			}
			continue
		}
		txt = append(txt, string(spacer) + str)
	}
	
	// Appends first half of the file.
	err = copyFile(f, tmp, 0, head)
	if err != nil { return err }
	
	// Inject new section when needed, and key = values.	
	txtL := len(txt) - 1
	for i, out := range txt {
		if i == 0 {
			if flag & cfg_HEADER == 0 {
				_, err = tmp.WriteString("\n" + out + "\n")
				if err != nil { return err }
				continue
			}
		}
		if i < txtL { 
			_, err = tmp.WriteString(out + ",\n") 
			if err != nil { return err }
		} else { 
			_, err = tmp.WriteString(out + "\n")
			if err != nil { return err }
		}
	}
	
	// Appends second half of file.
	if tail != -1 { 
		err = copyFile(f, tmp, tail-1, -1)
		if err != nil { return err }
	}

	// Sync and close everything.
	err = tmp.Sync()
	if err != nil { return err }

	err = tmp.Close()
	if err != nil { return err }

	tmp, err = os.Open(tmpfname)
	if err != nil { return err }

	err = f.Close()
	if err != nil { return err }
	
	destfile, err := os.OpenFile(file, os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil { return err }
	defer destfile.Close()

	_, err = io.Copy(destfile, tmp)
	if err != nil { return err }

	err = destfile.Sync()
	if err != nil {return err }

	err = tmp.Close()
	if err != nil { return err }

	err = os.Remove(tmpfname)
	if err != nil { return err }


	return nil
}
