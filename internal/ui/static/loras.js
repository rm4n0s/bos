// The LoRA table of the item dialog. The server decides which names fit
// (GET /ui/loras/suggest patches buttons into #lora-suggest); this file adds a
// chosen name as a row, removes a row, and moves through the list with the
// keyboard. The rows are plain form fields (lora_name, lora_model, lora_clip),
// so the dialog's form sends them like any other field.
(function () {
  'use strict';
  var SEARCH = 'lora-search';
  var LIST = 'lora-suggest';

  function byId(id) { return document.getElementById(id); }
  function buttons() {
    var l = byId(LIST);
    return Array.prototype.slice.call(l ? l.querySelectorAll('button') : []);
  }
  function active() { var l = byId(LIST); return l && l.querySelector('button.active'); }
  function clearList() { var l = byId(LIST); if (l) { l.textContent = ''; } }

  function activate(b) {
    buttons().forEach(function (x) { x.classList.remove('active'); x.removeAttribute('aria-selected'); });
    if (b) { b.classList.add('active'); b.setAttribute('aria-selected', 'true'); }
  }

  // Shows the table when it has a row and the "no LoRA" line when it has none.
  function refresh() {
    var rows = byId('lora-rows');
    if (!rows) { return; }
    var any = rows.querySelector('tr') !== null;
    var table = rows.closest('table');
    if (table) { table.hidden = !any; }
    var none = byId('lora-none');
    if (none) { none.hidden = any; }
  }

  function has(name) {
    var inputs = document.querySelectorAll('#lora-rows input[name="lora_name"]');
    for (var i = 0; i < inputs.length; i++) {
      if (inputs[i].value === name) { return true; }
    }
    return false;
  }

  function add(name) {
    var rows = byId('lora-rows');
    var tpl = byId('lora-row-template');
    if (!rows || !tpl || !name || has(name)) { return; }
    var row = tpl.content.cloneNode(true);
    row.querySelector('code').textContent = name;
    row.querySelector('input[name="lora_name"]').value = name;
    rows.appendChild(row);
    refresh();
  }

  function accept(b) {
    if (!b) { return; }
    add(b.dataset.name);
    var box = byId(SEARCH);
    if (box) { box.value = ''; box.focus(); }
    clearList();
  }

  // mousedown, not click: the box must keep the focus.
  document.addEventListener('mousedown', function (e) {
    var b = e.target.closest ? e.target.closest('#' + LIST + ' button') : null;
    if (!b) { return; }
    e.preventDefault();
    accept(b);
  });

  document.addEventListener('click', function (e) {
    var b = e.target.closest ? e.target.closest('[data-lora-remove]') : null;
    if (!b) { return; }
    var row = b.closest('tr');
    if (row) { row.remove(); }
    refresh();
  });

  document.addEventListener('keydown', function (e) {
    if (!e.target || e.target.id !== SEARCH) { return; }
    var all = buttons();
    var cur = active();
    var i = all.indexOf(cur);
    switch (e.key) {
      case 'ArrowDown':
        if (all.length === 0) { return; }
        e.preventDefault();
        activate(all[(i + 1) % all.length]);
        break;
      case 'ArrowUp':
        if (all.length === 0) { return; }
        e.preventDefault();
        activate(all[(i <= 0 ? all.length : i) - 1]);
        break;
      case 'Enter':
        // Never submit the dialog from the search box.
        e.preventDefault();
        accept(cur || all[0]);
        break;
      case 'Escape':
        if (all.length === 0) { return; }
        e.preventDefault();
        e.stopPropagation();
        clearList();
        break;
    }
  });

  document.addEventListener('focusout', function (e) {
    if (e.target && e.target.id === SEARCH) {
      // Let a click on a suggestion happen first.
      setTimeout(clearList, 150);
    }
  });
})();
