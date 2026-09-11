// Decorative pointer feedback. Fixed hit tiles keep scaling pixels from retriggering hover.
(() => {
  const logo = document.querySelector('.servo-wordmark svg');
  if (!logo) return;

  const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
  const active = new Map();
  const colors = ['#c5f277', '#89cbbd', '#edf7d8'];
  const offsets = [-4, -3, 3, 4];
  const pick = values => values[Math.floor(Math.random() * values.length)];

  function reset(pixel) {
    clearTimeout(active.get(pixel));
    active.delete(pixel);
    pixel.classList.remove('is-active');
  }

  function clear() {
    for (const pixel of active.keys()) reset(pixel);
  }

  logo.addEventListener('pointerover', event => {
    if (event.pointerType === 'touch') return;
    const pixel = event.target.closest('.wordmark-pixel');
    if (!pixel || pixel.contains(event.relatedTarget)) return;
    clearTimeout(active.get(pixel));

    // One reusable echo per visited pixel; no accumulating particles or animation loop.
    if (!pixel.querySelector('.wordmark-echo')) {
      const echo = pixel.querySelector('.wordmark-face').cloneNode();
      echo.setAttribute('class', 'wordmark-echo');
      pixel.prepend(echo);
    }
    pixel.style.setProperty('--pixel-color', pick(colors));
    pixel.style.setProperty('--pixel-scale', pick([0.75, 1.15, 1.25]));
    pixel.style.setProperty('--echo-x', `${pick(offsets)}px`);
    pixel.style.setProperty('--echo-y', `${pick(offsets)}px`);
    pixel.classList.add('is-active');
    active.set(pixel, null);
  });

  logo.addEventListener('pointerout', event => {
    const pixel = event.target.closest('.wordmark-pixel');
    if (!pixel || pixel.contains(event.relatedTarget) || !active.has(pixel)) return;
    clearTimeout(active.get(pixel));
    if (reducedMotion.matches) reset(pixel);
    else active.set(pixel, setTimeout(() => reset(pixel), 180));
  });

  logo.addEventListener('pointerleave', clear);
  logo.addEventListener('pointercancel', clear);
  window.addEventListener('blur', clear);
  reducedMotion.addEventListener('change', clear);
  document.addEventListener('visibilitychange', () => { if (document.hidden) clear(); });
})();
