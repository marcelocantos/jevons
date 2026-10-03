# Web cockpit settings

The React cockpit has an explicit **Settings** button in its top status bar.
It opens a dialog with the current same-origin server URL and connection status,
theme preference (System / Light / Dark), and a reset for the persisted sidebar
layout. The web client uses the origin serving the page; the URL is informational,
not an editable mobile-style server address. To connect to another server, open
that server's URL in the browser.

Do not use or reintroduce long-press as the web settings affordance. Long-press
is the native mobile entry point, not the browser UI contract.
