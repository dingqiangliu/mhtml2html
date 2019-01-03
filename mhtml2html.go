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

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/json"
	"github.com/tdewolff/minify/v2/svg"
	"github.com/tdewolff/minify/v2/xml"
	"github.com/tdewolff/parse/v2/buffer"
)

type file struct {
	contentType string
	base        *url.URL
	data        []byte
	initial     bool
	converted   bool
}


type arrayFlags []string

func (f *arrayFlags) String() string {
    return fmt.Sprint([]string(*f))
}

func (f *arrayFlags) Set(value string) error {
    *f = append(*f, value)
    return nil
}


var (
	browsing = flag.Bool("b", false, "optional: browsing result(default: false, ouput to stdout)") 
	removingElementArray arrayFlags //option: jquery like elements selectors to be removed
	removingAttrArray arrayFlags //option: pairs of jquery like elements selector and attribute to be removed
	needMinify = flag.Bool("m", false, "optional: need minify output(default: false)") 

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
            u =  "url(data:" + f.contentType + ";base64," + u + ")"
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

	// remove elements
	for _, sel := range removingElementArray {
		//fmt.Fprintf(os.Stderr, "DEBUG: remove elements %s: %s \n", sel, d.Find(sel).Nodes)
		d.Find(sel).Remove()
	}

	// remove attributes
	var sel, attr string
	for i, item := range removingAttrArray {
		if i % 2 == 0 {
			sel = item 
		} else {
			attr = item 
		}
		if i % 2 == 1 {
			el := d.Find(sel)
			//fmt.Fprintf(os.Stderr, "DEUBG: try to remove attr %s[%s] from: %s \n", sel, attr, el.Nodes)
			if _, ok := el.Attr(attr); ok {
				//el.RemoveAttr(attr)
				el.SetAttr(attr, "")
				//fmt.Fprintf(os.Stderr, "DEBUG:    removed attr %s[%s]\n", sel, attr)
			}
		}
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

			//fmt.Fprintf(os.Stderr, "DEBUG: Not found: %s ", url)
			return
		}
	}

	w.Header().Add("Content-Type", f.contentType)
	w.Header().Add("Content-Length", fmt.Sprint(len(f.data)))
	w.Write(f.data)

    //fmt.println(os.Stderr, "DEBUG GET: ", url, " Content-Type: ", f.contentType, "Content-Length: ", len(f.data))
}

func main() {
	log.SetFlags(log.Lshortfile)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "mhtl2html [opitons] MHTMLFILE \r\n Options: \r\n")
		flag.PrintDefaults()
	}
	flag.Var(&removingElementArray, "re", "repeatablely optional: jquery like elements selector to be removed")
	flag.Var(&removingAttrArray, "ra", "repeatablely optional: pairs of jquery like elements selector and attribute to be removed")
	flag.Parse()
	if (flag.NArg() != 1) || (len(removingAttrArray) % 2 != 0) {
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

	var minifier *minify.M
	if *needMinify {
		minifier = minify.New()
		minifier.AddFunc("text/css", css.Minify)
		minifier.AddFunc("text/html", html.Minify)
		minifier.AddFunc("image/svg+xml", svg.Minify)
		minifier.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
		minifier.AddFuncRegexp(regexp.MustCompile("[/+]json$"), json.Minify)
		minifier.AddFuncRegexp(regexp.MustCompile("[/+]xml$"), xml.Minify)
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

		// NOTE: no space and double quotation permitted in contenType of CSS url base64 encoding data
		contentType := part.Header.Get("Content-Type")
		contentType = strings.Replace(contentType, "\"", "", -1)
		contentType = strings.Replace(contentType, "'", "", -1)
		contentType = strings.Replace(contentType, " ", "", -1)
		initial := false
		if initialLoc == "" && (contentType == "text/html" || strings.HasPrefix(contentType, "text/html;")) {
			initialLoc = contentLocation
			initial = true
		}

		if *needMinify {
			out := buffer.NewWriter(make([]byte, 0, len(data)))
			if err := minifier.Minify(strings.Split(contentType, ";")[0], out, buffer.NewReader(data)); err == nil {
				data = out.Bytes()
			}  else {
				//fmt.Fprintf(os.Stderr, "DEBUG: failed to miniy %s %s. Reason: %s \n", contentType, contentLocation, err)
			}
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
			fmt.Fprintf(os.Stderr, "Couldn't start browser:", err)
            fmt.Fprintf(os.Stderr, "Open the following URL manually:", initialURL)
	    } else {
            fmt.Fprintf(os.Stderr, "Browsing: %s", initialURL)
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

