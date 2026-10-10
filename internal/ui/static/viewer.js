// The picture viewer of the job list. A click on a result picture (a link with
// data-viewer) opens it over the page, and the buttons, the arrow keys or a
// swipe move to the previous and the next picture of the list. A click with a
// modifier key, or a middle click, still opens the picture in a new tab.
//
// The list is the whole collection, not the page on the screen: the table says
// where to ask for it (data-pictures). The viewer starts with the pictures of
// the page, so it opens at once, and swaps in the whole list when it arrives. At
// the last picture it asks again, so pictures that finished meanwhile are
// reached too. If the server cannot be asked, the pictures of the page remain.
(function () {
  'use strict';
  var SELECTOR = '#jobs-table a[data-viewer]';
  var root = null;     // the overlay, made on first use
  var image, caption, count, prev, next, close, open;
  var list = [];       // the pictures to walk through: {href, label}
  var current = null;  // href of the picture shown
  var position = 0;    // its index, for when the list changed under it
  var opener = null;   // what had the focus before

  function links() { return Array.prototype.slice.call(document.querySelectorAll(SELECTOR)); }

  function fromPage() {
    return links().map(function (l) { return { href: l.getAttribute('href'), label: l.getAttribute('data-viewer') || '' }; });
  }

  // load asks the server for every picture of the collection; it gives null
  // when there is no answer.
  function load() {
    var table = document.getElementById('jobs-table');
    var url = table && table.getAttribute('data-pictures');
    if (!url || !window.fetch) { return Promise.resolve(null); }
    return fetch(url, { credentials: 'same-origin', headers: { 'Accept': 'application/json' } })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (rows) {
        if (!Array.isArray(rows) || rows.length === 0) { return null; }
        return rows.map(function (p) { return { href: p.url, label: p.label || '' }; });
      })
      .catch(function () { return null; });
  }

  // refresh swaps in the whole collection and redraws the picture that is shown.
  function refresh() {
    return load().then(function (all) {
      if (!all || !root || root.hidden) { return; }
      list = all;
      draw();
    });
  }

  function button(label, cls, text) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = cls;
    b.setAttribute('aria-label', label);
    b.textContent = text;
    return b;
  }

  function build() {
    root = document.createElement('div');
    root.className = 'viewer';
    root.hidden = true;
    root.setAttribute('role', 'dialog');
    root.setAttribute('aria-modal', 'true');
    root.setAttribute('aria-label', 'Picture viewer');

    image = document.createElement('img');
    image.className = 'viewer-image';
    image.alt = '';
    caption = document.createElement('span');
    count = document.createElement('span');
    count.className = 'muted';
    open = document.createElement('a');
    open.target = '_blank';
    open.rel = 'noopener';
    open.textContent = 'open in a new tab';
    var bar = document.createElement('div');
    bar.className = 'viewer-bar';
    bar.appendChild(caption);
    bar.appendChild(count);
    bar.appendChild(open);

    prev = button('Previous picture', 'viewer-nav viewer-prev', '‹');
    next = button('Next picture', 'viewer-nav viewer-next', '›');
    close = button('Close', 'viewer-close', '×');

    root.appendChild(image);
    root.appendChild(bar);
    root.appendChild(prev);
    root.appendChild(next);
    root.appendChild(close);
    document.body.appendChild(root);

    prev.addEventListener('click', function (e) { e.stopPropagation(); step(-1); });
    next.addEventListener('click', function (e) { e.stopPropagation(); step(1); });
    close.addEventListener('click', function (e) { e.stopPropagation(); hide(); });
    // A click beside the picture closes the viewer.
    root.addEventListener('click', function (e) { if (e.target === root) { hide(); } });

    var startX = null;
    root.addEventListener('touchstart', function (e) { startX = e.touches.length === 1 ? e.touches[0].clientX : null; }, { passive: true });
    root.addEventListener('touchend', function (e) {
      if (startX === null || !e.changedTouches.length) { return; }
      var dx = e.changedTouches[0].clientX - startX;
      startX = null;
      if (Math.abs(dx) > 50) { step(dx < 0 ? 1 : -1); }
    }, { passive: true });
  }

  function indexOfCurrent() {
    for (var i = 0; i < list.length; i++) {
      if (list[i].href === current) { return i; }
    }
    // The picture is not in the list: stay near where it was.
    return Math.min(position, list.length - 1);
  }

  // draw shows the picture at the current place of the list.
  function draw() {
    var i = indexOfCurrent();
    var item = list[i];
    if (!item) { return; }
    current = item.href;
    position = i;
    if (image.getAttribute('src') !== current) { image.src = current; }
    image.alt = item.label;
    caption.textContent = item.label;
    count.textContent = (i + 1) + ' / ' + list.length;
    open.href = current;
    prev.disabled = i === 0;
    next.disabled = i === list.length - 1;
    // Load the neighbours now, so the next click is instant.
    [list[i - 1], list[i + 1]].forEach(function (p) { if (p) { new Image().src = p.href; } });
  }

  function show(i) {
    current = list[i].href;
    draw();
  }

  function step(by) {
    if (list.length === 0) { hide(); return; }
    var i = indexOfCurrent() + by;
    if (i < 0) { return; }
    if (i >= list.length) {
      // At the end: some pictures may have finished since the list was loaded.
      var before = list.length;
      refresh().then(function () {
        var j = indexOfCurrent() + 1;
        if (list.length > before && j < list.length) { show(j); }
      });
      return;
    }
    show(i);
  }

  function reveal(link) {
    if (!root) { build(); }
    opener = document.activeElement;
    root.hidden = false;
    document.body.classList.add('viewer-open');
    list = fromPage();
    current = link.getAttribute('href');
    position = links().indexOf(link);
    if (position < 0) {
      // A picture that is not on the page (the dialog of a job): show it, and
      // the whole list will put it in its place.
      list.unshift({ href: current, label: link.getAttribute('data-viewer') || '' });
      position = 0;
    }
    draw();
    close.focus();
    refresh();
  }

  function hide() {
    if (!root || root.hidden) { return; }
    root.hidden = true;
    document.body.classList.remove('viewer-open');
    image.removeAttribute('src');
    current = null;
    if (opener && opener.focus) { opener.focus(); }
  }

  document.addEventListener('click', function (e) {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) { return; }
    var link = e.target.closest ? e.target.closest('a[data-viewer]') : null;
    if (!link || link.closest('.viewer')) { return; }
    e.preventDefault();
    // The detail dialog's picture is not in the list: show the list's one for the same job.
    var same = links().filter(function (l) { return l.getAttribute('href') === link.getAttribute('href'); })[0];
    reveal(same || link);
  });

  document.addEventListener('keydown', function (e) {
    if (!root || root.hidden) { return; }
    switch (e.key) {
      case 'ArrowLeft': e.preventDefault(); step(-1); break;
      case 'ArrowRight': e.preventDefault(); step(1); break;
      case 'Escape': e.preventDefault(); e.stopPropagation(); hide(); break;
    }
  }, true);
})();
