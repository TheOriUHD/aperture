# Sandbox isolation probe (test fixture)

A deliberately adversarial UI extension used to verify the extension sandbox by
hand in a browser. Its panel (`ui/panel.html`) tries to read `document.cookie`,
reach into the parent-page DOM, and `fetch('/api/v1/...')` directly — all of
which must be **blocked** by the iframe sandbox + CSP — then confirms the
sanctioned `window.aperture` broker path still works.

This is a diagnostic fixture, **not** an example to install for real use. Zip the
directory and install it via Settings → Extensions → Install file, then open the
Dashboard card it renders.
