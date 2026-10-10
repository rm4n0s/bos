// The picture viewer of the job list. A click on a result picture (a link with
// data-viewer) opens it over the page, and the buttons, the arrow keys or a
// swipe move to the previous and the next picture of the list. A click with a
// modifier key, or a middle click, still opens the picture in a new tab. It
// does not talk to the server; the list is whatever is on the page, so the
// pictures that arrive while the page is open are in it.
(function () {
  'use strict';
  var SELECTOR = '#jobs-table a[data-viewer]';
  var root = null;     // the overlay, made on first use
  var image, caption, count, prev, next, close, open;
  var current = null;  // href of the picture shown
  var position = 0;    // its index, for when the list changed under it
  var opener = null;   // what had the focus before

  function links() { return Array.prototype.slice.call(document.querySelectorAll(SELECTOR)); }

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

  function indexOfCurrent(all) {
    for (var i = 0; i < all.length; i++) {
      if (all[i].getAttribute('href') === current) { return i; }
    }
    // The list was redrawn without the picture: stay near where it was.
    return Math.min(position, all.length - 1);
  }

  function show(link) {
    var all = links();
    var i = all.indexOf(link);
    if (i < 0) { return; }
    current = link.getAttribute('href');
    position = i;
    image.src = current;
    image.alt = link.getAttribute('data-viewer') || '';
    caption.textContent = link.getAttribute('data-viewer') || '';
    count.textContent = (i + 1) + ' / ' + all.length;
    open.href = current;
    prev.disabled = i === 0;
    next.disabled = i === all.length - 1;
    // Load the neighbours now, so the next click is instant.
    [all[i - 1], all[i + 1]].forEach(function (l) { if (l) { new Image().src = l.getAttribute('href'); } });
  }

  function step(by) {
    var all = links();
    if (all.length === 0) { hide(); return; }
    var i = indexOfCurrent(all) + by;
    if (i < 0 || i >= all.length) { return; }
    show(all[i]);
  }

  function reveal(link) {
    if (!root) { build(); }
    opener = document.activeElement;
    root.hidden = false;
    document.body.classList.add('viewer-open');
    show(link);
    close.focus();
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
