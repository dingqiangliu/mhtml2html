# mhtml2html

Convert MHTM file to a single HTML embedded all resources with base64 encoding, so that your Spotlight or other desktop search engine can index them by default.

## Update
 
- 2019-01-03: support removing elements or attributes.

- 2019-01-01: embed all resources in a single HTML file, including scripts, style sheets and fonts.

- 2018-12-31: clone from[UnMHT at gitlab](https://gitlab.com/opennota/unmht) .

## Install

``` BASH
go get -u github.com/dingqiangliu/mhtml2html
```

## Usage

``` BASH
# mhtml2html -h
mhtl2html [options] MHTMLFILE
  -b	optional: browsing result(default: false, ouput to stdout)
  -ra value
    	repeatablely optional: pairs of jquery like elements selector and attribute to be removed
  -re value
    	repeatablely optional: jquery like elements selector to be removed

# convert MHTL file to a single HTML file.
mhtml2html previously-saved.mht > singlefile.html

# open MHTL with default browser, no matter it support MHTL format or not.
mhtml2html -b previously-saved.mht 
```

