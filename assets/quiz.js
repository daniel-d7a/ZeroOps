// quiz widget: questions = [{q, options: [..], answer: index, why}]
// Reusable component — every lesson includes this instead of inlining quiz JS.
function renderQuiz(el, questions) {
  el.classList.add('quiz');
  questions.forEach((qq, qi) => {
    const box = document.createElement('div');
    box.className = 'quiz-q';
    const h = document.createElement('p');
    h.innerHTML = `<strong>${qi + 1}. ${qq.q}</strong>`;
    box.appendChild(h);
    const opts = document.createElement('div');
    opts.className = 'quiz-options';
    let answered = false;
    qq.options.forEach((opt, oi) => {
      const b = document.createElement('button');
      b.textContent = opt;
      b.onclick = () => {
        if (answered) return;
        answered = true;
        const fb = document.createElement('div');
        fb.className = 'quiz-feedback';
        if (oi === qq.answer) {
          b.classList.add('correct');
          fb.className += ' ok';
          fb.textContent = 'Correct. ' + (qq.why || '');
        } else {
          b.classList.add('wrong');
          box.querySelectorAll('button')[qq.answer].classList.add('correct');
          fb.className += ' no';
          fb.textContent = 'Not quite. ' + (qq.why || '');
        }
        box.appendChild(fb);
      };
      opts.appendChild(b);
    });
    box.appendChild(opts);
    el.appendChild(box);
  });
}
document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('[data-quiz]').forEach(el => {
    renderQuiz(el, JSON.parse(el.getAttribute('data-quiz')));
  });
});
