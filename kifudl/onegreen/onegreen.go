package onegreen

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"github.com/missdeer/golib/ic"
	"github.com/missdeer/golib/semaphore"
	"github.com/missdeer/weiqitools/kifudl/util"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	client *http.Client
)

type Onegreen struct {
	sync.WaitGroup
	*semaphore.Semaphore
	SaveFileEncoding string
	quit             bool // assume it's false as initial value
	QuitIfExists     bool
	DownloadCount    int32
}

type Page struct {
	URL   string
	Count int
}

func (o *Onegreen) downloadKifu(sgf string) {
	o.Add(1)
	o.Acquire()
	defer func() {
		o.Release()
		o.Done()
	}()
	if o.quit {
		return
	}
	retry := 0

	req, err := http.NewRequest("GET", sgf, nil)
	if err != nil {
		log.Println("onegreen - Could not parse kifu request:", err)
		return
	}

	req.Header.Set("Referer", "http://game.onegreen.net/weiqi/ShowClass.asp?ClassID=1218&page=1254")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64; rv:45.0) Gecko/20100101 Firefox/45.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("accept-language", `en-US,en;q=0.8`)
	req.Header.Set("Upgrade-Insecure-Requests", "1")
doRequest:
	resp, err := client.Do(req)
	if err != nil {
		log.Println("onegreen - Could not send kifu request:", err)
		retry++
		if retry < 3 {
			time.Sleep(3 * time.Second)
			goto doRequest
		}
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("onegreen - kifu request not 200")
		retry++
		if retry < 3 {
			time.Sleep(3 * time.Second)
			goto doRequest
		}
		return
	}
	kifu, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("onegreen - cannot read kifu content", err)
		retry++
		if retry < 3 {
			time.Sleep(3 * time.Second)
			goto doRequest
		}
		return
	}

	// extract SGF data
	index := bytes.Index(kifu, []byte(`sgftext=`))
	if index < 0 {
		log.Println("onegreen - cannot find start keyword")
		return
	}
	kifu = kifu[index+8:]
	index = bytes.Index(kifu, []byte(`" ALLOWSCRIPTACCESS=`))
	if index < 0 {
		log.Println("onegreen - cannot find end keyword")
		return
	}
	kifu = kifu[:index]

	u, err := url.Parse(sgf)
	if err != nil {
		log.Fatal(err)
	}
	fullPath := "onegreen/" + u.Path[1:]
	fullPath = strings.Replace(fullPath, ".html", ".sgf", -1)
	insertPos := len(fullPath) - 7
	fullPathByte := []byte(fullPath)
	fullPathByte = append(fullPathByte[:insertPos], append([]byte{'/'}, fullPathByte[insertPos:]...)...)
	fullPath = string(fullPathByte)
	if util.Exists(fullPath) {
		if !o.quit && o.QuitIfExists {
			log.Println(fullPath, " exists, just quit")
			o.quit = true
		}
		return
	}

	dir := path.Dir(fullPath)
	if !util.Exists(dir) {
		os.MkdirAll(dir, 0777)
	}

	if o.SaveFileEncoding != "gbk" {
		kifu = ic.Convert("gbk", o.SaveFileEncoding, kifu)
	}
	ioutil.WriteFile(fullPath, kifu, 0644)
	kifu = nil
	atomic.AddInt32(&o.DownloadCount, 1)
}

func (o *Onegreen) downloadPage(page string) {
	o.Add(1)
	o.Acquire()
	defer func() {
		o.Release()
		o.Done()
	}()
	retry := 0
	req, err := http.NewRequest("GET", page, nil)
	if err != nil {
		log.Println("onegreen - Could not parse page request:", err)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 6.1; WOW64; rv:45.0) Gecko/20100101 Firefox/45.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("accept-language", `en-US,en;q=0.8`)
doPageRequest:
	resp, err := client.Do(req)
	if err != nil {
		log.Println("onegreen - Could not send page request:", err)
		retry++
		if retry < 3 {
			time.Sleep(3 * time.Second)
			goto doPageRequest
		}
		return
	}

	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("onegreen - cannot read page content", err)
		retry++
		if retry < 3 {
			time.Sleep(3 * time.Second)
			goto doPageRequest
		}
		return
	}

	regex := regexp.MustCompile(`href='(http:\/\/game\.onegreen\.net\/weiqi\/HTML\/[0-9a-zA-Z\-\_]+\.html)'`)
	ss := regex.FindAllSubmatch(data, -1)
	for _, match := range ss {
		if o.quit {
			break
		}
		sgf := string(match[1])
		go o.downloadKifu(sgf)
	}
}

func (o *Onegreen) Download(w *sync.WaitGroup) {
	defer w.Done()
	client = &http.Client{
		Timeout: 60 * time.Second,
	}

	pagelist := []Page{
		{"http://game.onegreen.net/weiqi/ShowClass.asp?ClassID=1218&page=%d", 2000},
		{"http://game.onegreen.net/weiqi/ShowClass.asp?ClassID=1223&page=%d", 514},
	}

	for _, page := range pagelist {
		if o.quit {
			break
		}
		for i := 1; !o.quit && i <= page.Count; i++ {
			u := fmt.Sprintf(page.URL, i)
			o.downloadPage(u)
		}
	}

	o.Wait()
	fmt.Println("downloaded", o.DownloadCount, " SGF files from onegreen")
}
