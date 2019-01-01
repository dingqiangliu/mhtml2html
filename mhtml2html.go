package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"
	"github.com/pkg/browser"
)

type file struct {
	contentType string
	base        *url.URL
	data        []byte
	initial     bool
	converted   bool
}

var (
	browsing = flag.Bool("b", false, "browsing result(default: false)")

	rWithProto = regexp.MustCompile("^[a-z]+:")
	rURL       = regexp.MustCompile(`\burl\(([^()]+)\)`)

	files   = make(map[string]*file)
	cid2loc = make(map[string]string)
)

func abs(base *url.URL, url string) string {
	if rWithProto.MatchString(url) {
		return url
	}
	if strings.HasPrefix(url, "//") {
		return base.Scheme + ":" + url
	}
	if strings.HasPrefix(url, "/") {
		return base.Scheme + "://" + base.Host + url
	}
	return base.Scheme + "://" + base.Host + path.Join("/", path.Dir(base.Path), url)
}

func modifyCSS(base *url.URL, data []byte) []byte {
	return rURL.ReplaceAllFunc(data, func(d []byte) []byte {
		u := string(d[4 : len(d)-1])
		u = strings.Trim(u, `"'`)
		if strings.HasPrefix(u, "data:") || strings.HasPrefix(u, "mailto:") {
			return d
		}
		if strings.HasPrefix(u, "cid:") {
			cid := strings.TrimPrefix(u, "cid:")
			u = cid2loc[cid]
		}
        // try to embed resource in CSS 
        if f, ok := files[u]; ok {
            u =  base64.StdEncoding.EncodeToString(f.data)
            ct := strings.Replace(f.contentType, "\"", "", -1)
            ct = strings.Replace(ct, " ", "", -1)
            u =  "url(data:" + ct + ";base64," + u + ")"
        } else {
		    u = "url(/" + url.PathEscape(abs(base, u)) + ")"
        }
		return []byte(u)
	})
}

func modifyHTML(base *url.URL, data []byte, converted bool) ([]byte, error) {
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if converted {
		d.Find("meta[http-equiv], meta[charset]").Each(func(_ int, sel *goquery.Selection) {
			httpEq, ok := sel.Attr("http-equiv")
			if ok && strings.ToLower(httpEq) == "content-type" {
				sel.SetAttr("content", "text/html; charset=utf-8")
			} else if !ok {
				sel.SetAttr("charset", "utf-8")
			}
		})
	}

	redefinedBase := d.Find("head > base[href]")
	if redefinedBase.Length() > 0 {
		href, _ := redefinedBase.First().Attr("href")
		u, err := url.Parse(href)
		if err != nil {
			return nil, err
		}
		base = u
		redefinedBase.Remove()
	}

	m := cascadia.MustCompile("a[href]")
	for _, attr := range []string{"src", "href", "background"} {
		d.Find("[" + attr + "]").Each(func(_ int, sel *goquery.Selection) {
			if sel.IsMatcher(m) {
				return
			}
			v, _ := sel.Attr(attr)
			if strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "mailto:") {
				return
			}
			if strings.HasPrefix(v, "cid:") {
				cid := strings.TrimPrefix(v, "cid:")
				v = cid2loc[cid]
			}

            // try to embed resource in HTML
            if f, ok := files[v]; ok {
                v =  "data:" + f.contentType + ";base64," + base64.StdEncoding.EncodeToString(f.data)
			    sel.SetAttr(attr, v)
            } else {
			    v = abs(base, v)
			    sel.SetAttr(attr, "/"+url.PathEscape(v))
            }

			sel.RemoveAttr("integrity")
		})
	}
	d.Find("style").Each(func(_ int, sel *goquery.Selection) {
		style := sel.Text()
		if !strings.Contains(style, "url(") {
			return
		}
		style = string(modifyCSS(base, []byte(style)))
		sel.SetText(style)
	})
	d.Find("[style]").Each(func(_ int, sel *goquery.Selection) {
		style, _ := sel.Attr("style")
		if !strings.Contains(style, "url(") {
			return
		}
		style = string(modifyCSS(base, []byte(style)))
		sel.SetAttr("style", style)
	})

	html, err := d.Html()
	if err != nil {
		return nil, err
	}
	return []byte(html), nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimPrefix(r.URL.Path, "/")
	f, ok := files[url]
	if !ok {
		found := false
		lower := strings.ToLower(url)
		for k, f2:= range files {
			if strings.ToLower(k) == lower {
				found = true
				f = f2
				break
			}
		}
		if !found {
			http.NotFound(w, r)

            //log.Println("Not found: %s ", url)
			return
		}
	}

	w.Header().Add("Content-Type", f.contentType)
	w.Header().Add("Content-Length", fmt.Sprint(len(f.data)))
	w.Write(f.data)

    //log.Println("GET: ", url, " Content-Type: ", f.contentType, "Content-Length: ", len(f.data))
}

func main() {
	log.SetFlags(log.Lshortfile)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "USAGE: mhtl2html [-b] FILE")
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(0)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}

	msg, err := mail.ReadMessage(f)
	if err != nil {
		log.Fatal(err)
	}

	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	boundary := params["boundary"]
	if err != nil {
		log.Fatal(err)
	}

	mpr := multipart.NewReader(msg.Body, boundary)
	initialLoc := ""
	for {
		part, err := mpr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		contentLocation := part.Header.Get("Content-Location")
		base, err := url.Parse(contentLocation)
		if err != nil {
			log.Fatal(err)
		}

		data, err := ioutil.ReadAll(part)
		if err != nil {
			log.Fatal(err)
		}

		if part.Header.Get("Content-Transfer-Encoding") == "base64" {
			n := base64.StdEncoding.DecodedLen(len(data))
			buf := make([]byte, n)
			n, err := base64.StdEncoding.Decode(buf, data)
			if err != nil {
				log.Fatal(err)
			}
			data = buf[:n]
		}

		if cid := part.Header.Get("Content-ID"); cid != "" {
			cid = strings.Trim(cid, "<>")
			cid2loc[cid] = contentLocation
		}

		contentType := part.Header.Get("Content-Type")
		initial := false
		if initialLoc == "" && (contentType == "text/html" || strings.HasPrefix(contentType, "text/html;")) {
			initialLoc = contentLocation
			initial = true
		}
		files[contentLocation] = &file{contentType, base, data, initial, false}
	}

	if initialLoc == "" {
		log.Fatal("no HTML pages to display")
	}

	for _, f := range files {
		if ct := f.contentType; ct == "text/css" {
			f.data = modifyCSS(f.base, f.data)
        }
    }
	for _, f := range files {
		if ct := f.contentType; ct == "text/html" || strings.HasPrefix(ct, "text/html;") {
			encoding, name, certain := charset.DetermineEncoding(f.data, f.contentType)
			if name != "utf-8" && !(name == "windows-1252" && !certain) {
				decoded, err := encoding.NewDecoder().Bytes(f.data)
				if err != nil {
					log.Fatal(err)
				}
				f.data = decoded
				if strings.Contains(f.contentType, ";") {
					f.contentType = strings.SplitN(f.contentType, ";", 2)[0]
				}
				f.converted = true
			}
			var err error
			f.data, err = modifyHTML(f.base, f.data, f.converted)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

    if *browsing {
	    srv := httptest.NewServer(http.HandlerFunc(handler))
	    initialURL := srv.URL + "/" + url.PathEscape(initialLoc)
	    if err := browser.OpenURL(initialURL); err != nil {
			log.Println("Couldn't start browser:", err)
            log.Println("Open the following URL manually:", initialURL)
	    } else {
            log.Println("Browsing: ", initialURL)
        }

	    select {}
    } else {
	    for _, f := range files {
		    if ct := f.contentType; ct == "text/html" || strings.HasPrefix(ct, "text/html;") {
                os.Stdout.Write(f.data)
            }
        }
    }
}

