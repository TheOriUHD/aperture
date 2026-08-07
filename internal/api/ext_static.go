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
	low := strings.ToLower(html)
	if i := strings.Index(low, "</head>"); i >= 0 {
		return []byte(html[:i] + extSDKBootstrap + html[i:])
	}
	if i := strings.Index(low, "<body>"); i >= 0 {
		return []byte(html[:i+6] + extSDKBootstrap + html[i+6:])
	}
	return []byte(extSDKBootstrap + html)
}

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
  window.aperture={
    request:req,
    cameras:{list:function(){return req('GET','/cameras');},get:function(id){return req('GET','/cameras/'+id);}},
    events:{list:function(q){return req('GET','/events'+(q?('?'+q):''));}},
    resize:function(h){parent.postMessage({__apReq:true,type:'ui.resize',payload:{height:h}},'*');}
  };
  try{var ro=new ResizeObserver(function(){window.aperture.resize(document.documentElement.scrollHeight);});ro.observe(document.documentElement);}catch(e){}
})();</script>`
