package mkvlib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strings"
)

const (
	mkvmerge   = `mkvmerge`
	mkvextract = `mkvextract`
)

type mkvInfo struct {
	Attachments []struct {
		ID          int    `json:"id"`
		FileName    string `json:"file_name"`
		Size        int    `json:"size"`
		ContentType string `json:"content_type"`
	} `json:"attachments"`
	Tracks []struct {
		ID         int    `json:"id"`
		Type       string `json:"type"`
		Codec      string `json:"codec"`
		Properties struct {
			Language  string `json:"language"`
			TrackName string `json:"track_name"`
		} `json:"properties"`
	}
}

type mkvProcessor bool

func (self *mkvProcessor) GetMKVInfo(file string) *mkvInfo {
	buf := bytes.NewBufferString("")
	if p, err := newProcess(nil, buf, nil, "", mkvmerge, "-J", file); err == nil {
		if s, err := p.Wait(); err == nil && s.ExitCode() == 0 {
			obj := new(mkvInfo)
			_ = json.Unmarshal(buf.Bytes(), obj)
			return obj
		}
	}
	return nil
}

func (self *mkvProcessor) DumpMKV(file, output string, subset bool) bool {
	ec := 0
	obj := self.GetMKVInfo(file)
	if obj == nil {
		log.Printf(`Failed to get the mkv file info: "%s".`, file)
		return false
	}
	attachments := make([]string, 0)
	tracks := make([]string, 0)
	for _, _item := range obj.Attachments {
		attachments = append(attachments, fmt.Sprintf(`%d:%s`, _item.ID, path.Join(output, "fonts", _item.FileName)))
	}
	for _, _item := range obj.Tracks {
		if _item.Type == "subtitles" {
			s := fmt.Sprintf(`%d_%s_%s`, _item.ID, _item.Properties.Language, _item.Properties.TrackName)
			if _item.Codec == "SubStationAlpha" {
				s += ".ass"
			} else {
				s += ".sub"
			}
			tracks = append(tracks, fmt.Sprintf(`%d:%s`, _item.ID, path.Join(output, s)))
		}
	}
	args := make([]string, 0)
	args = append(args, file)
	args = append(args, "attachments")
	args = append(args, attachments...)
	args = append(args, "tracks")
	args = append(args, tracks...)
	if p, err := newProcess(nil, nil, nil, "", mkvextract, args...); err == nil {
		s, err := p.Wait()
		ok := err == nil && s.ExitCode() == 0
		if ok {
			if subset {
				asses := make([]string, 0)
				for _, _item := range tracks {
					_arr := strings.Split(_item, ":")
					f := _arr[len(_arr)-1]
					if strings.HasSuffix(f, ".ass") {
						asses = append(asses, f)
					}
					if len(asses) > 0 {
						if !self.ASSFontSubset(asses, "", "", false) {
							ec++
						}
					}
				}
			}
		} else {
			ec++
		}
	} else {
		ec++
	}
	return ec == 0
}

func (self *mkvProcessor) CheckSubset(file string) (bool, bool) {
	obj := self.GetMKVInfo(file)
	if obj == nil {
		log.Printf(`Failed to get the mkv file info: "%s".`, file)
		return false, true
	}
	ass := false
	ok := false
	reg, _ := regexp.Compile(`\.[A-Z0-9]{8}\.\S+$`)
	for _, track := range obj.Tracks {
		ass = track.Type == "subtitles" && track.Codec == "SubStationAlpha"
		if ass {
			break
		}
	}
	for _, attachment := range obj.Attachments {
		ok = !ass || (strings.HasPrefix(attachment.ContentType, "font/") && reg.MatchString(attachment.FileName))
		if ok {
			break
		}
	}
	return !ass || (ass && ok), false
}

func (self *mkvProcessor) CreateMKV(file string, tracks, attachments []string, output, slang, stitle string, clean bool) bool {
	args := make([]string, 0)
	args = append(args, "--output", output)
	if clean {
		args = append(args, "--no-subtitles", "--no-attachments")
	}
	args = append(args, file)
	for _, _item := range attachments {
		args = append(args, "--attach-file", _item)
	}
	for _, _item := range tracks {
		_, _, _, f := splitPath(_item)
		_arr := strings.Split(f, "_")
		_sl := slang
		_st := stitle
		if len(_arr) > 1 {
			_sl = _arr[1]
		}
		if len(_arr) > 2 {
			_st = _arr[2]
		}
		if _sl != "" {
			args = append(args, "--language", "0:"+_sl)
		}
		if _st != "" {
			args = append(args, "--track-name", "0:"+_st)
		}
		args = append(args, _item)
	}
	if p, err := newProcess(nil, nil, nil, "", mkvmerge, args...); err == nil {
		s, err := p.Wait()
		return err == nil && s.ExitCode() == 0
	}
	return false
}

func (self *mkvProcessor) DumpMKVs(dir, output string, subset bool) bool {
	ec := 0
	files := findMKVs(dir)
	l := len(files)
	for i, item := range files {
		p := strings.TrimPrefix(item, dir)
		d, _, _, f := splitPath(p)
		p = path.Join(output, d, f)
		if !self.DumpMKV(item, p, subset) {
			ec++
			log.Printf(`Failed to dump the mkv file "%s".`, item)
		}
		log.Printf("Dump (%d/%d) done.", i+1, l)
	}
	return ec == 0
}

func (self *mkvProcessor) QueryFolder(dir string) []string {
	ec := 0
	lines := make([]string, 0)
	files := findMKVs(dir)
	l := len(files)
	for i, file := range files {
		a, b := self.CheckSubset(file)
		if b {
			ec++
		} else if !a {
			lines = append(lines, file)
		}
		log.Printf("Query (%d/%d) done.", i+1, l)
	}
	return lines
}

func (self *mkvProcessor) CreateMKVs(vDir, sDir, fDir, tDir, oDir string, slang, stitle string, clean bool) bool {
	ec := 0
	if tDir == "" {
		tDir = os.TempDir()
	}
	tDir = path.Join(tDir, randomStr(8))
	files, _ := findPath(vDir, fmt.Sprintf(`\.\S+$`))
	l := len(files)
	for i, item := range files {
		_, _, _, _f := splitPath(item)
		tmp, _ := findPath(sDir, fmt.Sprintf(`%s\S*\.\S+$`, regexp.QuoteMeta(_f)))
		asses := make([]string, 0)
		subs := make([]string, 0)
		p := path.Join(tDir, _f)
		for _, sub := range tmp {
			if strings.HasSuffix(sub, ".ass") {
				_, _, _, __f := splitPath(sub)
				_s := path.Join(p, __f) + ".ass"
				_ = copyFileOrDir(sub, _s)
				asses = append(asses, _s)
			} else {
				subs = append(subs, sub)
			}
		}
		attachments := make([]string, 0)
		tracks := make([]string, 0)
		if len(asses) > 0 {
			_ = os.RemoveAll(tDir)
			if !self.ASSFontSubset(asses, fDir, "", false) {
				ec++
			} else {
				__p := path.Join(p, "subsetted")
				attachments = findFonts(__p)
				tracks, _ = findPath(__p, `\.ass$`)
			}
		}
		tracks = append(tracks, subs...)
		fn := path.Join(oDir, _f) + ".mkv"
		if !self.CreateMKV(item, tracks, attachments, fn, slang, stitle, clean) {
			ec++
		}
		if ec > 0 {
			log.Printf(`Failed to create the mkv file: "%s".`, item)
		}
		log.Printf("Create (%d/%d) done.", i+1, l)
	}
	_ = os.RemoveAll(tDir)
	return ec == 0
}

func (self *mkvProcessor) MakeMKVs(dir, data, output, slang, sttlte string) bool {
	ec := 0
	files := findMKVs(dir)
	l := len(files)
	for i, item := range files {
		p := strings.TrimPrefix(item, dir)
		d, n, _, f := splitPath(p)
		p = path.Join(data, d, f)
		_p := path.Join(p, "subsetted")
		subs, _ := findPath(p, `\.sub`)
		asses, _ := findPath(_p, `\.ass$`)
		attachments := findFonts(_p)
		tracks := append(subs, asses...)
		fn := path.Join(output, d, n)
		if !self.CreateMKV(item, tracks, attachments, fn, slang, sttlte, true) {
			ec++
			log.Printf(`Faild to make the mkv file: "%s".`, item)
		}
		log.Printf("Make (%d/%d) done.", i+1, l)
	}
	return ec == 0
}

func (self *mkvProcessor) ASSFontSubset(files []string, fonts, output string, dirSafe bool) bool {
	if len(files) == 0 {
		return false
	}
	obj := new(assProcessor)
	obj.files = files
	obj._fonts = fonts
	obj.output = output
	d, _, _, _ := splitPath(obj.files[0])
	if obj._fonts == "" {
		obj._fonts += path.Join(d, "fonts")
	}
	if obj.output == "" {
		obj.output = d
		dirSafe = true
	}
	if dirSafe {
		obj.output = path.Join(obj.output, "subseted")
	}
	obj.fonts = findFonts(obj._fonts)

	return obj.parse() && obj.matchFonts() && obj.createFontsSubset() && obj.changeFontsName() && obj.replaceFontNameInAss()
}