// Autocomplete of code names in the command box. The server decides what fits
// (POST /ui/commands/complete) and patches the buttons into #command-suggest;
// this file only inserts a chosen name and moves through the list with the
// keyboard. It does not talk to the server.
(function () {
  'use strict';
  var BOX = 'command-code';
  var LIST = 'command-suggest';

  function list() { return document.getElementById(LIST); }
  function buttons() { return Array.prototype.slice.call(list() ? list().querySelectorAll('button') : []); }
  function active() { return list() && list().querySelector('button.active'); }

  function clear() {
    var l = list();
    if (l) { l.textContent = ''; }
  }

  function activate(b) {
    buttons().forEach(function (x) { x.classList.remove('active'); x.removeAttribute('aria-selected'); });
    if (b) { b.classList.add('active'); b.setAttribute('aria-selected', 'true'); }
  }

  function accept(b) {
    var box = document.getElementById(BOX);
    if (!box || !b) { return; }
    box.focus();
    box.setRangeText(b.dataset.name, Number(b.dataset.start), Number(b.dataset.end), 'end');
    // Let the preview and the next suggestions see the new text.
    box.dispatchEvent(new Event('input', { bubbles: true }));
    box.dispatchEvent(new KeyboardEvent('keyup', { key: 'End', bubbles: true }));
  }

  // mousedown, not click: the box must keep the focus.
  document.addEventListener('mousedown', function (e) {
    var b = e.target.closest ? e.target.closest('#' + LIST + ' button') : null;
    if (!b) { return; }
    e.preventDefault();
    accept(b);
  });

  document.addEventListener('keydown', function (e) {
    if (!e.target || e.target.id !== BOX) { return; }
    var all = buttons();
    if (all.length === 0) { return; }
    var cur = active();
    var i = all.indexOf(cur);
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        activate(all[(i + 1) % all.length]);
        break;
      case 'ArrowUp':
        e.preventDefault();
        activate(all[(i <= 0 ? all.length : i) - 1]);
        break;
      case 'Tab':
        if (e.shiftKey) { return; }
        e.preventDefault();
        accept(cur || all[0]);
        break;
      case 'Enter':
        // Enter takes a suggestion only after the arrows chose one; otherwise it is a line break.
        if (cur) { e.preventDefault(); accept(cur); }
        break;
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        clear();
        break;
    }
  });

  document.addEventListener('focusout', function (e) {
    if (e.target && e.target.id === BOX) { clear(); }
  });
})();
