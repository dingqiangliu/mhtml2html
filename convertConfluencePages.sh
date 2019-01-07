#!/bin/sh

scriptDir=$(cd "$(dirname $0)"; pwd)

function convertFile() 
{
  f="$1"
  html="${f%.mht*}"
  html="${html%.htm*}.html"
  nt=$(date -r $(stat -f "%m" "$f") -v "+1S" "+%Y%m%d%H%M.%S")
  if [ ! -n "$(find "$(dirname "$f")" -name "$(basename "$html")" -newer "$f")" ] ; then 
    ver=$(grep "ian Confluence " "$f"|sed -rn 's/.*Confluence\s([0-9.]*).*/\1/p')

    failed=
    case "$ver" in
    "3.3")
      "$scriptDir/mhtml2html" -m -re '#splitter-sidebar' -re '[class=vsplitbar]' \
        -re '#footer' -re '#header' -re '#navigation' -re 'img[class="logo global"]' \
        -re 'li[class="page-metadata-item noprint"]' -re '#comment-top-links' \
        -re '#add-comment-bottom' -re '[class=comment-actions]' -re '#labels-edit' \
        -ra 'html' -ra 'class' -ra 'body' -ra 'class' -ra '#splitter' -ra 'style' \
        -ra '#splitter-content' -ra 'style' "$f" > "$html" || failed=true
      ;;
      
    "5.1.3")
      #"$scriptDir/mhtml2html" -m -re '#splitter-sidebar' -re '[class=vsplitbar]' -re '#footer' -re '#header' -re '#navigation' -re 'img[class="logo global"]' -re 'li[class="page-metadata-item noprint"]' -re '#comment-top-links' -re '#add-comment-bottom' -re '[class=comment-actions]' -re '#labels-edit' -ra 'html' -ra 'class' -ra 'body' -ra 'class' -ra '#splitter' -ra 'style' -ra '#splitter-content' -ra 'style' "$f" > "$html" || failed=true
      # 5.1.3 for MapR
      "$scriptDir/mhtml2html" -m -re '#splitter-sidebar' -re '[class=vsplitbar]' \
        -re '#footer' -re '#header' -re '#navigation' -ra 'html' -ra 'class' \
        -ra 'body' -ra 'class' -ra '#splitter' -ra 'style' \
        -ra '#splitter-content' -ra 'style' "$f" > "$html" || failed=true
      ;;
      
    "5.6.5")
      "$scriptDir/mhtml2html" -m -re '#splitter-sidebar' -re '[class=ia-fixed-sidebar]' -re '[class=vsplitbar]' \
        -re '#footer' -re '#header' -re '#navigation' -re 'img[class="logo global"]' \
        -re '#likes-section' -re '#labels-edit' -re '[class="bottom-comment-panels comment-panels"]' \
        -re '[class="first action-reply-comment"]' -re '[class=comment-action-like]' \
        -ra 'html' -ra 'class' -ra 'body' -ra 'class' -ra '#main' -ra 'style' -ra '#splitter' -ra 'style' \
        -ra '#splitter-content' -ra 'style' "$f" > "$html"  || failed=true
      ;;
      
    "5.8.4")
      "$scriptDir/mhtml2html" -m -re '[class="ia-fixed-sidebar"]' -re '#footer' \
        -re '#header' -re '#navigation' -re 'img[class="logo global"]' \
        -re '#likes-section' -re '#labels-edit' -re '[class="bottom-comment-panels comment-panels"]' \
        -re '[class="first action-reply-comment"]' -re '[class=comment-action-like]' \
        -ra 'html' -ra 'class' -ra 'body' -ra 'class' -ra '#main' -ra 'style' \
        -ra '#splitter' -ra 'style' -ra '#splitter-content' -ra 'style' "$f" > "$html" || failed=true
      ;;
      
    "6.7.2")
      "$scriptDir/mhtml2html" -m -re '[class=ia-splitter-left]' -re '#footer' \
        -re '#header' -re '#breadcrumb-section' -re '#page-metadata-banner' -re '#navigation' -re '#likes-section' \
        -re '[class="bottom-comment-panels comment-panels"]' \
        -re '[class="first action-reply-comment"]' -re '[class=comment-action-like]' \
        -ra 'html' -ra 'class' -ra 'body' -ra 'class' -ra '#main' -ra 'style' "$f" > "$html" || failed=true
      ;;
    *)
      "$scriptDir/mhtml2html" -m "$f" > "$html" || failed=true
      ;;
    esac
    
    if [ -n "$failed" ] ; then
      echo "FAILED: $f" >&2
      [ -f "$html" ] && rm -rf "$html"
    else
      touch -m -t "$nt" "$html"
    fi
  fi
}


function convertPath() 
{
  path="$1"
  while read f ; do
    convertFile "$f"
  done <<< "$(find "$path" -name '*.mht*' 2>/dev/null)"
}


function clearFile() 
{
  f="$1"
  TMPDIR="REMOVED/$(dirname "$f")"
  mht="${f}.mht"
  if [ -n "$(find "$(dirname "$f")" -name "$(basename "$mht*")" ! -newer "$f")" ] ; then 
    mkdir -p "$TMPDIR"
    mv -vf "$mht"* "$TMPDIR"
  fi
  mht="${f%.htm*}.mht"
  if [ -n "$(find "$(dirname "$f")" -name "$(basename "$mht*")" ! -newer "$f")" ] ; then 
    mkdir -p "$TMPDIR"
    mv -vf "$mht"* "$TMPDIR"
  fi
}


function clearPath() 
{
  path="$1"
  while read f ; do
    clearFile "$f"
  done <<< "$(find "$path" -name '*.htm*' 2>/dev/null)"
}


showUsage ()
{
  echo "Convert all *.MHT* files in specified path to HTML format in the same location. \r\n  Usage: $0 [-r(remove older *mht*)] path" >&2
}


removeOldMHT=
while getopts ":rh:" opt; do
  case $opt in
    r)
      removeOldMHT="true"
      ;;
    h|?)
      showUsage
      exit 1
      ;;
  esac
done
shift $(($OPTIND -1)) 

if [ $# -ne 1 ] ; then
  showUsage
  exit 1
fi

loc="$1"
if [ -d "$loc" ] ; then
  if [ -n "$removeOldMHT" ] ; then
    clearPath "$loc"
  else
    convertPath "$loc"
  fi
elif [ -f "$loc" ] ; then
  if [ -n "$removeOldMHT" ] ; then
    clearFile "$loc"
  else
    convertFile "$loc"
  fi
else
  echo "ERROR: $loc not exist!" >&2
  showUsage
  exit 1
fi

