// NineLives site: replay terminal, copy buttons, hex dump, nav highlight.
// Every page section reads fine without this file; it only adds motion and
// interaction.
(function () {
  "use strict";

  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // ---- recorded sessions -------------------------------------------------
  // Captured from real runs of ./bin/ninelives, ./bin/torture and go test,
  // pasted unchanged. "[exit n]" lines were added by the capture script to
  // show a non-zero exit status; the programs did not print them.
  var scenes = {
    tour: [
      "$ ./bin/ninelives open demo.db",
      "keys=0 recovery_ms=0",
      "$ ./bin/ninelives put demo.db user:1 hello",
      "OK seq=1",
      "$ ./bin/ninelives put demo.db user:2 x:00ff",
      "OK seq=2",
      "$ ./bin/ninelives get demo.db user:1",
      "hello",
      "$ ./bin/ninelives get demo.db user:99",
      "NOTFOUND",
      "$ ./bin/ninelives del demo.db user:1",
      "OK seq=3",
      "$ printf 'put k1 v1\\nput k2 v2\\ndel k1\\n' > ops.txt",
      "$ ./bin/ninelives batch demo.db ops.txt",
      "OK seq=4",
      "$ ./bin/ninelives dump demo.db",
      "k2 v2",
      "user:2 x:00ff",
      "$ ./bin/ninelives check demo.db",
      "OK"
    ],
    crash: [
      "$ ./bin/ninelives put crash.db a 1",
      "OK seq=1",
      "$ ./bin/ninelives put crash.db b 2",
      "OK seq=2",
      "$ NINELIVES_CRASH_AT=mid_write ./bin/ninelives put crash.db c 3",
      "[exit 137]",
      "$ ./bin/ninelives check crash.db",
      "CORRUPT: ninelives: corrupt file: torn tail at offset 74 (file is 86 bytes)",
      "[exit 1]",
      "$ ./bin/ninelives open crash.db",
      "keys=2 recovery_ms=0",
      "$ ./bin/ninelives check crash.db",
      "OK",
      "$ ./bin/ninelives dump crash.db",
      "a 1",
      "b 2"
    ],
    torture: [
      "$ ./bin/torture --seed 85123 --iterations 8",
      "torture: seed=85123 mode=default iterations=8, oracle log runs/85123/oracle.log",
      "iter 1/8 ok  delay=369ms sent=28551 acked=28550 err=0 inflight=1 keys=50 recovery_ms=6",
      "iter 2/8 ok  delay=230ms sent=18032 acked=18032 err=0 inflight=0 keys=50 recovery_ms=3",
      "iter 3/8 ok  delay=93ms sent=6986 acked=6986 err=0 inflight=0 keys=43 recovery_ms=4",
      "iter 4/8 ok  delay=48ms sent=3755 acked=3753 err=0 inflight=2 keys=44 recovery_ms=5",
      "iter 5/8 ok  delay=163ms sent=12490 acked=12489 err=0 inflight=1 keys=47 recovery_ms=0",
      "iter 6/8 ok  delay=113ms sent=8234 acked=8232 err=0 inflight=2 keys=44 recovery_ms=2",
      "iter 7/8 ok  delay=141ms sent=10850 acked=10848 err=0 inflight=2 keys=45 recovery_ms=4",
      "iter 8/8 ok  delay=64ms sent=4943 acked=4943 err=0 inflight=0 keys=43 recovery_ms=7",
      "PASS 8/8 iterations (seed 85123, mode default)"
    ],
    million: [
      "$ go test ./tests/engine -run TestRecoveryMillionKeysUnder2s -v -count=1",
      "=== RUN   TestRecoveryMillionKeysUnder2s",
      "    recovery_bench_test.go:59: 1M keys, 127 MB file: open 407.69316ms (Stats.RecoveryTime 407.692812ms)",
      "--- PASS: TestRecoveryMillionKeysUnder2s (0.59s)",
      "PASS",
      "ok  \tninelives/tests/engine\t0.593s"
    ]
  };

  // Pause before an output line, in ms: the real waits, shortened.
  var outputDelay = { torture: 260, million: 650 };

  function esc(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  // classFor picks the colour of one output line.
  function classFor(line) {
    if (/^\[exit \d+\]$/.test(line)) return "x";
    if (/^(OK\b|PASS\b|--- PASS)/.test(line)) return "ok";
    if (/^(CORRUPT|FAIL|--- FAIL)/.test(line)) return "err";
    return "";
  }

  function lineHTML(line) {
    if (line.indexOf("$ ") === 0) return '<span class="ps">$</span> ' + esc(line.slice(2));
    var c = classFor(line);
    return c ? '<span class="' + c + '">' + esc(line) + "</span>" : esc(line);
  }

  var term = document.getElementById("replay");
  if (term) setupReplay(term);

  function setupReplay(fig) {
    var body = fig.querySelector(".term-body");
    var tabs = fig.querySelectorAll('[role="tab"]');
    var replay = fig.querySelector(".term-replay");
    var run = 0; // bumps on every start, so an old playback stops itself
    var current = "tour";
    var started = false;

    function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }

    function render(done, partial) {
      var html = done.map(lineHTML).join("\n");
      if (partial !== undefined) {
        html += (html ? "\n" : "") + '<span class="ps">$</span> ' + esc(partial);
      }
      body.innerHTML = html + (partial !== undefined || !reduceMotion ? '<span class="cursor" aria-hidden="true"></span>' : "");
      body.scrollTop = body.scrollHeight;
    }

    async function play(name) {
      var me = ++run;
      var lines = scenes[name];
      current = name;
      replay.hidden = false;
      if (reduceMotion) { render(lines); return; }
      var done = [];
      for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        if (line.indexOf("$ ") === 0) {
          var cmd = line.slice(2);
          await sleep(i ? 380 : 250);
          for (var k = 1; k <= cmd.length; k++) {
            if (me !== run) return;
            render(done, cmd.slice(0, k));
            await sleep(14 + (k * 7) % 23);
          }
          await sleep(140);
        } else {
          await sleep(outputDelay[name] || 45);
        }
        if (me !== run) return;
        done.push(line);
        render(done);
      }
    }

    function select(name) {
      tabs.forEach(function (t) {
        var on = t.getAttribute("data-scene") === name;
        t.setAttribute("aria-selected", on ? "true" : "false");
        t.tabIndex = on ? 0 : -1;
      });
      play(name);
    }

    tabs.forEach(function (t, i) {
      t.tabIndex = i === 0 ? 0 : -1;
      t.addEventListener("click", function () { started = true; select(t.getAttribute("data-scene")); });
      t.addEventListener("keydown", function (e) {
        var d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
        if (!d) return;
        e.preventDefault();
        var next = tabs[(i + d + tabs.length) % tabs.length];
        next.focus();
        next.click();
      });
    });
    replay.addEventListener("click", function () { play(current); });

    // Start the first session when the terminal scrolls into view.
    if ("IntersectionObserver" in window) {
      var io = new IntersectionObserver(function (entries) {
        if (entries[0].isIntersecting && !started) {
          started = true;
          io.disconnect();
          play("tour");
        }
      }, { threshold: 0.35 });
      io.observe(fig);
    } else {
      started = true;
      play("tour");
    }
  }

  // ---- copy buttons ------------------------------------------------------
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand("copy"); } catch (e) { ok = false; }
      document.body.removeChild(ta);
      ok ? resolve() : reject(new Error("copy failed"));
    });
  }

  document.querySelectorAll(".copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      copyText(btn.getAttribute("data-copy")).then(function () {
        btn.textContent = "copied";
        btn.setAttribute("data-done", "");
      }, function () {
        btn.textContent = "select + copy";
      });
      setTimeout(function () { btn.textContent = "copy"; btn.removeAttribute("data-done"); }, 1600);
    });
  });

  // ---- hex dump ----------------------------------------------------------
  // The real 80-byte file from `put demo.db a hello` + `put demo.db b x:0001ff`.
  var hexBytes = "4e4c563101000000010000000000000000000000dfde4f0571283cc70c0000000101000000000000000100610500000068656c6c6fc99a12e30a000000010200000000000000010062030000000001ff";
  // [first byte, last byte, name, meaning]
  var fields = [
    [0, 3, "magic", "<code>NLV1</code>: marks a NineLives file."],
    [4, 5, "format version", "1"],
    [6, 7, "flags", "0, unused"],
    [8, 15, "SeqBase", "1: the next sequence number when this file was created."],
    [16, 19, "reserved", "0"],
    [20, 23, "header CRC32-C", "<code>054fdedf</code> over bytes 0x00–0x13. A bad header means this is not a NineLives file."],
    [24, 27, "record 1 · CRC32-C", "<code>c73c2871</code> over bytes 0x1c–0x34. Recovery keeps the record only if this matches."],
    [28, 31, "record 1 · length", "12 payload bytes"],
    [32, 32, "record 1 · type", "01 = PUT"],
    [33, 40, "record 1 · seq", "1"],
    [41, 42, "record 1 · key length", "1"],
    [43, 43, "record 1 · key", "<code>a</code>"],
    [44, 47, "record 1 · value length", "5"],
    [48, 52, "record 1 · value", "<code>hello</code>"],
    [53, 56, "record 2 · CRC32-C", "<code>e3129ac9</code> over bytes 0x39–0x4f."],
    [57, 60, "record 2 · length", "10 payload bytes"],
    [61, 61, "record 2 · type", "01 = PUT"],
    [62, 69, "record 2 · seq", "2"],
    [70, 71, "record 2 · key length", "1"],
    [72, 72, "record 2 · key", "<code>b</code>"],
    [73, 76, "record 2 · value length", "3"],
    [77, 79, "record 2 · value", "bytes <code>00 01 ff</code>, written as <code>x:0001ff</code>"]
  ];

  var hex = document.getElementById("hex");
  if (hex) setupHex(hex);

  function setupHex(box) {
    var pre = box.querySelector(".hex-dump");
    var info = box.querySelector(".hex-info");
    var n = hexBytes.length / 2;
    var fieldOf = [];
    fields.forEach(function (f, i) { for (var b = f[0]; b <= f[1]; b++) fieldOf[b] = i; });

    var out = [];
    for (var row = 0; row < n; row += 16) {
      var line = '<span class="off">' + ("0000000" + row.toString(16)).slice(-8) + ":</span> ";
      var asc = "";
      for (var b = row; b < row + 16 && b < n; b++) {
        var v = parseInt(hexBytes.substr(b * 2, 2), 16);
        var ch = v >= 32 && v < 127 ? esc(String.fromCharCode(v)) : ".";
        line += '<span class="b" data-i="' + b + '">' + hexBytes.substr(b * 2, 2) + "</span>";
        if (b % 2 === 1) line += " ";
        asc += '<span class="b asc" data-i="' + b + '">' + ch + "</span>";
      }
      out.push(line + " " + asc);
    }
    pre.innerHTML = "<code>" + out.join("\n") + "</code>";
    pre.removeAttribute("aria-hidden");
    pre.tabIndex = 0;
    pre.setAttribute("aria-label", "Hex dump of an 80-byte NineLives file. Use the left and right arrow keys to move between fields.");

    var spans = pre.querySelectorAll(".b");
    var active = -1;

    function show(fi) {
      if (fi === active || fields[fi] === undefined) return;
      active = fi;
      var f = fields[fi];
      spans.forEach(function (s) {
        var i = +s.getAttribute("data-i");
        s.classList.toggle("on", i >= f[0] && i <= f[1]);
      });
      var range = f[0] === f[1] ? "byte 0x" + f[0].toString(16) : "bytes 0x" + f[0].toString(16) + "–0x" + f[1].toString(16);
      info.innerHTML = "<b>" + f[2] + "</b> <span class=\"dim\">(" + range + ")</span><br>" + f[3];
    }

    function pick(e) {
      var t = e.target.closest(".b");
      if (t) show(fieldOf[+t.getAttribute("data-i")]);
    }
    pre.addEventListener("mouseover", pick);
    pre.addEventListener("click", pick);
    pre.addEventListener("focus", function () { if (active < 0) show(0); });
    pre.addEventListener("keydown", function (e) {
      var d = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
      if (!d) return;
      e.preventDefault();
      show(Math.min(fields.length - 1, Math.max(0, (active < 0 ? 0 : active) + d)));
    });
  }

  // ---- nav: mark the section in view ---------------------------------------
  var links = {};
  document.querySelectorAll(".tree a").forEach(function (a) { links[a.getAttribute("href").slice(1)] = a; });
  if ("IntersectionObserver" in window) {
    var visible = {};
    var navIO = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) { visible[e.target.id] = e.isIntersecting; });
      var first = null;
      document.querySelectorAll("main section").forEach(function (s) { if (!first && visible[s.id]) first = s.id; });
      if (!first) return;
      Object.keys(links).forEach(function (id) {
        if (id === first) {
          links[id].setAttribute("aria-current", "true");
          if (links[id].scrollIntoView && window.innerWidth < 1080) {
            var nav = links[id].closest(".tree");
            nav.scrollLeft = links[id].offsetLeft - 16;
          }
        } else {
          links[id].removeAttribute("aria-current");
        }
      });
    }, { rootMargin: "-90px 0px -55% 0px" });
    document.querySelectorAll("main section").forEach(function (s) { navIO.observe(s); });
  }
})();
