// Polls pending deliveries and updates their status pill in place.
(function () {
  function poll() {
    var pending = document.querySelectorAll("[data-delivery]");
    if (!pending.length) return;
    var finished = 0;
    pending.forEach(function (el) {
      fetch("/api/deliveries/" + el.dataset.delivery, { credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (d) {
          if (!d) return;
          el.textContent = d.label;
          el.className = "pill " + d.tone;
          if (d.done) {
            el.removeAttribute("data-delivery");
            finished++;
          }
        })
        .catch(function () {});
    });
    setTimeout(function () {
      // Reload once something finished so account facts are fresh.
      if (finished > 0 && !document.querySelector("form input:focus")) location.reload();
      else poll();
    }, 1500);
  }
  document.addEventListener("DOMContentLoaded", function () { setTimeout(poll, 1000); });
})();

// Buttons with data-confirm ask before submitting.
document.addEventListener("click", function (ev) {
  var b = ev.target.closest && ev.target.closest("[data-confirm]");
  if (b && !window.confirm(b.dataset.confirm)) ev.preventDefault();
});

// Multi-select of items to remove: select-all per table, counter and confirm text.
(function () {
  var button = document.querySelector("[data-remove-button]");
  if (!button) return;
  var counter = document.querySelector("[data-remove-count]");
  var boxes = function (root) { return root.querySelectorAll('input[name="sel"]'); };
  function refresh() {
    var n = document.querySelectorAll('input[name="sel"]:checked').length;
    counter.textContent = n === 0 ? "Nenhum item selecionado" : n === 1 ? "1 item selecionado" : n + " itens selecionados";
    button.disabled = n === 0;
    button.dataset.confirm = n === 1 ? "Remover o item selecionado?" : "Remover os " + n + " itens selecionados?";
  }
  document.addEventListener("change", function (ev) {
    var t = ev.target;
    if (t.matches("[data-select-all]")) {
      boxes(t.closest("table")).forEach(function (b) { b.checked = t.checked; });
    }
    if (t.matches("[data-select-all]") || t.matches('input[name="sel"]')) refresh();
  });
  refresh();
})();
