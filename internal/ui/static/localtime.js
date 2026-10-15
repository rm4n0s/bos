// Shows every <time datetime> in the viewer's own timezone. The server writes
// UTC text as a fallback; this replaces it. Datastar patches add elements
// later, so a MutationObserver converts those too.
(function () {
  'use strict';
  var fmt = new Intl.DateTimeFormat(undefined, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false
  });

  function local(d) {
    var p = {};
    fmt.formatToParts(d).forEach(function (x) { p[x.type] = x.value; });
    return p.year + '-' + p.month + '-' + p.day + ' ' + p.hour + ':' + p.minute;
  }

  function convertOne(el) {
    if (el.hasAttribute('data-local')) { return; }
    var d = new Date(el.getAttribute('datetime'));
    if (isNaN(d.getTime())) { return; }
    el.textContent = local(d);
    el.title = d.toISOString();
    el.setAttribute('data-local', '');
  }

  function convert(node) {
    if (node.matches && node.matches('time[datetime]')) { convertOne(node); }
    Array.prototype.forEach.call(node.querySelectorAll('time[datetime]'), convertOne);
  }

  function start() {
    convert(document);
    new MutationObserver(function (records) {
      records.forEach(function (r) {
        Array.prototype.forEach.call(r.addedNodes, function (n) { if (n.nodeType === 1) { convert(n); } });
      });
    }).observe(document.body, { childList: true, subtree: true });
  }

  if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', start); } else { start(); }
}());
