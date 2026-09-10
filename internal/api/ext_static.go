package api

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// extCSP restricts an extension UI document to a self-contained panel: inline
// script/style only (its own bundle), data/blob images, NO network of its own (it
// talks to the host via postMessage, never fetch), and it can only be framed by
// the app. Combined with the host rendering it in an iframe with
// sandbox="allow-scripts" (no allow-same-origin → opaque origin, no parent access,
// no cookies), a malicious panel is structurally unable to reach the session or
// the API except through the broker.
const extCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

// handleExtAsset serves an enabled extension's UI files from disk (the ui/ subtree
// only). It is unauthenticated: the sandboxed iframe sends no cookie, so the assets
// (installed by an admin, not secret) are public while the extension is enabled;
// all DATA access is gated by the authed broker. Mounted at /ext/{id}/* below the
// API routes and before the SPA catch-all.
func (s *Server) handleExtAsset(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		http.NotFound(w, r)
		return
	}
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil || !ex.Enabled {
		http.NotFound(w, r)
		return
	}
	// Only the ui/ subtree is web-served (never backend code, the manifest, the
	// import map, or the token env). path.Clean collapses any traversal so a
	// ../-laden request cannot escape ui/.
	clean := path.Clean("/" + chi.URLParam(r, "*"))
	if !strings.HasPrefix(clean, "/ui/") {
		http.NotFound(w, r)
		return
	}
	base := filepath.Clean(s.extDir(id))
	full := filepath.Join(base, filepath.FromSlash(clean))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", extCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// Inject the SDK runtime into an HTML entry so `window.aperture` is available
	// inline (it posts to the host, which relays to the broker — the panel never
	// fetches Aperture directly). Non-HTML assets are served untouched.
	if strings.HasSuffix(strings.ToLower(full), ".html") {
		b, err := os.ReadFile(full)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(injectSDK(string(b)))
		return
	}
	http.ServeFile(w, r, full)
}

func injectSDK(html string) []byte {
	inject := extPanelBase + extSDKBootstrap
	low := strings.ToLower(html)
	if i := strings.Index(low, "</head>"); i >= 0 {
		return []byte(html[:i] + inject + html[i:])
	}
	if i := strings.Index(low, "<body>"); i >= 0 {
		return []byte(html[:i+6] + inject + html[i+6:])
	}
	// A panel with no <head> or <body> is legal HTML and the docs' own starter
	// omits both. Prepending here would put a <script> BEFORE the doctype, which
	// drops the document into quirks mode — and a quirks-mode panel loses the
	// transparent canvas that overlay slots depend on, so it paints an opaque
	// rectangle over the video instead of floating on it. Insert after the doctype
	// so the declaration stays first.
	if i := strings.Index(low, "<!doctype"); i >= 0 {
		if end := strings.Index(html[i:], ">"); end >= 0 {
			at := i + end + 1
			return []byte(html[:at] + inject + html[at:])
		}
	}
	return []byte(inject + html)
}

// extPanelBase pins a panel document to the app's own colour scheme.
//
// This is what keeps an overlay panel TRANSPARENT, and it is not obvious: per CSS
// Color Adjust §2.4 the UA must paint an OPAQUE canvas whenever an iframe ELEMENT's
// used color-scheme differs from the embedded document's root element. color-scheme
// INHERITS, so a panel frame computes `dark` from the app's :root while a panel that
// declares nothing computes light — and on that mismatch the spec replaces the
// transparent canvas outright. The panel's own `background: transparent` is
// necessary but NOT sufficient; without this the frame paints a white rectangle over
// the video no matter what the panel does.
//
// Injected for every panel so third-party bundles get it without changing their code.
// `:root` (specificity 0-1-0) outranks a panel's own `html` rule (0-0-1), so it holds
// regardless of which branch above placed it.
const extPanelBase = `<style>:root{color-scheme:dark}</style>`

// extSDKBootstrap is the @aperture/sdk runtime for a UI panel (T1.7, iframe side).
// It exposes window.aperture, whose calls postMessage to the host; the host relays
// them to /api/v1/ext/{id}/broker under the caller∩extension authority and posts
// the reply back. The panel thus reaches the API only through the sanctioned,
// scope-checked path — a direct fetch would be blocked by the sandbox + CSP.
const extSDKBootstrap = `<script>(function(){
  var pending={},seq=0;
  window.addEventListener('message',function(e){
    var m=e.data||{}; if(!m.__ap||!(m.id in pending))return;
    var p=pending[m.id]; delete pending[m.id];
    if(m.error)p.reject(new Error(m.error));else p.resolve(m.result);
  });
  function call(t,p){return new Promise(function(res,rej){var id=++seq;pending[id]={resolve:res,reject:rej};parent.postMessage({__apReq:true,id:id,type:t,payload:p},'*');});}
  function req(method,path,body){return call('api.request',{method:method,path:path,body:body});}
  var camId=null; try{camId=new URLSearchParams(location.search).get('camera');}catch(e){}
  window.aperture={
    cameraId:camId,
    request:req,
    cameras:{list:function(){return req('GET','/cameras');},get:function(id){return req('GET','/cameras/'+id);}},
    events:{list:function(q){return req('GET','/events'+(q?('?'+q):''));}},
    storage:{
      get:function(k){return call('storage.get',{key:k});},
      set:function(k,v){return call('storage.set',{key:k,value:v});},
      delete:function(k){return call('storage.delete',{key:k});},
      keys:function(){return call('storage.keys',{});}
    },
    settings:{
      get:function(k){return call('settings.get',{key:k});},
      all:function(){return call('settings.all',{});}
    },
    net:{
      fetch:function(url,init){init=init||{};return call('net.fetch',{url:url,method:init.method,headers:init.headers,body:init.body});}
    },
    resize:function(h,w){parent.postMessage({__apReq:true,type:'ui.resize',payload:{height:h,width:w}},'*');}
  };
  try{
    var measure=function(){var b=document.body;window.aperture.resize(b?b.scrollHeight:document.documentElement.scrollHeight);};
    var ro=new ResizeObserver(measure);
    ro.observe(document.documentElement);
    document.addEventListener("DOMContentLoaded",function(){if(document.body)ro.observe(document.body);measure();});
  }catch(e){}
})();</script>`
