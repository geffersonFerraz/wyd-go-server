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