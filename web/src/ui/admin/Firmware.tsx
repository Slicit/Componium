/* Putting the node firmware on a board, from this page.
 *
 * Web Serial, through esp-web-tools, which is vendored rather than pulled from
 * a CDN: this is the page you reach for when a rig is half built and the wifi
 * is the thing you are trying to fix.
 *
 * The one thing worth understanding before reading further: Web Serial only
 * exists in a secure context. The studio is served over plain HTTP on a home
 * network, so `navigator.serial` is simply undefined here, and no amount of
 * asking will conjure it. That is not a bug to route around, it is the
 * browser refusing to hand a page on an unauthenticated origin a USB device.
 * So the page detects it and says how to get one, rather than rendering a
 * button that silently does nothing.
 */

import { useEffect, useState } from 'react';

/** Where the vendored flasher lives, copied verbatim by the build. */
const TOOLS = '/esp-web-tools/install-button.js';

interface Available {
  available: boolean;
  /** Why not, when it is not. */
  why?: string;
  manifest?: string;
  name?: string;
  bytes?: number;
}

/** Whether this browser will give a page a serial port at all. */
function serialUsable(): boolean {
  return typeof navigator !== 'undefined' && 'serial' in navigator;
}

function Megabytes({ of }: { of: number }) {
  return <>{(of / (1024 * 1024)).toFixed(2)} MB</>;
}

export function Firmware() {
  const [build, setBuild] = useState<Available | null>(null);
  const [loaded, setLoaded] = useState(false);
  const usable = serialUsable();

  useEffect(() => {
    let alive = true;
    void fetch('/api/firmware')
      .then((r) => (r.ok ? r.json() : { available: false, why: 'the studio has no firmware directory' }))
      .then((b) => { if (alive) setBuild(b); })
      .catch(() => { if (alive) setBuild({ available: false, why: 'the studio did not answer' }); });
    return () => { alive = false; };
  }, []);

  /* Loaded once, and only where it can do something. The module registers a
   * custom element, so importing it twice is a redefinition error rather than
   * a wasted request. */
  useEffect(() => {
    if (!usable || !build?.available || loaded) return;
    if (document.querySelector('script[data-esp-web-tools]')) { setLoaded(true); return; }
    const el = document.createElement('script');
    el.type = 'module';
    el.src = TOOLS;
    el.dataset.espWebTools = 'yes';
    el.onload = () => setLoaded(true);
    document.head.appendChild(el);
  }, [usable, build, loaded]);

  return (
    <div className="adm-page">
      <h2>Node firmware</h2>
      <p className="dim">
        One board, two instruments, two protocols. The fan takes CIP over UDP,
        with a watchdog that puts the output back to safe if the conductor stops
        talking for 300&nbsp;ms. The strip takes sACN, exactly as a WLED
        controller does, so a rig entry for it is a WLED entry with a different
        address in it.
      </p>

      <section className="adm-card">
        <h3>The build</h3>
        {!build && <p className="dim small">looking…</p>}
        {build && !build.available && (
          <>
            <p className="adm-warn">No firmware to flash: {build.why}.</p>
            <p className="dim small">Build it on a machine with ESP-IDF, then point the studio at it:</p>
            <pre className="adm-code">{`cd firmware/esp32
idf.py set-target esp32
idf.py build
./make-web-install.sh          # writes firmware/web/

componium studio -firmware firmware/web ...`}</pre>
          </>
        )}
        {build?.available && (
          <dl className="adm-facts">
            <dt>image</dt><dd>{build.name ?? 'componium node'}</dd>
            <dt>size</dt><dd>{build.bytes ? <Megabytes of={build.bytes} /> : 'unknown'}</dd>
            <dt>chip</dt><dd>ESP32</dd>
          </dl>
        )}
      </section>

      <section className="adm-card">
        <h3>Flashing</h3>
        {!usable && (
          <>
            <p className="adm-warn">
              This browser will not hand the page a USB device, because the studio
              is being served over plain HTTP.
            </p>
            <p className="dim small">
              Web Serial needs a secure context, and <code>localhost</code> counts
              as one. Tunnel the studio to your own machine and open it there:
            </p>
            <pre className="adm-code">{`ssh -L 8722:localhost:8722 claude@claude-machine-02.home`}</pre>
            <p className="dim small">
              Then open <code>http://localhost:8722/#/admin/firmware</code> in
              Chrome or Edge. Firefox and Safari have no Web Serial at all.
            </p>
          </>
        )}
        {usable && !build?.available && (
          <p className="dim small">Nothing to flash yet.</p>
        )}
        {usable && build?.available && (
          <>
            <ol className="adm-steps">
              <li>Plug the ESP32 into this machine over USB.</li>
              <li>
                Press <em>Install</em> and pick the serial port that appears. A
                board that already has this firmware does not need installing
                again: the same dialog offers <em>Connect to Wi-Fi</em> and a
                console for a board it recognises.
              </li>
              <li>
                When it finishes, the same dialog offers <em>Connect to Wi-Fi</em>.
                Your password goes down the cable into the board’s own storage.
                It is not sent anywhere and it is not in the image.
              </li>
              <li>
                The board reports its address when it joins. Put that in the rig:
                <code> addr = "192.168.1.x:5570"</code>.
              </li>
            </ol>
            {loaded ? (
              /* @ts-expect-error a custom element, registered by the module above */
              <esp-web-install-button manifest={build.manifest ?? '/firmware/manifest.json'}>
                <button slot="activate" className="adm-go">Install</button>
                <span slot="unsupported" className="adm-warn">
                  This browser has no Web Serial. Chrome or Edge.
                </span>
                <span slot="not-allowed" className="adm-warn">
                  Serial is blocked here. A secure context is required.
                </span>
              {/* @ts-expect-error closing the same custom element */}
              </esp-web-install-button>
            ) : (
              <p className="dim small">loading the flasher…</p>
            )}
          </>
        )}
      </section>

      <section className="adm-card">
        <h3>Wiring</h3>
        <dl className="adm-facts">
          <dt>LED strip</dt>
          <dd>
            Data to <strong>GPIO&nbsp;5</strong>. WS2812 timing, 30 pixels by
            default. The strip takes its 5&nbsp;V from its own supply, not from
            the board, and the grounds must be common. It is driven as one
            colour across the whole length, which is what an ambient wash is:
            the conductor sends a fixture’s colour, not a pixel array.
          </dd>
          <dt>Fan</dt>
          <dd>
            PWM on <strong>GPIO&nbsp;18</strong> at 25&nbsp;kHz, above hearing,
            which is what a four pin fan expects.
          </dd>
        </dl>
        <dl className="adm-facts">
          <dt>4&nbsp;pin fan</dt>
          <dd>
            GPIO&nbsp;18 to the fan’s PWM pin (usually blue). The fan takes 12&nbsp;V
            from its own supply; the board never carries fan current. Grounds
            must be common.
          </dd>
          <dt>2 or 3&nbsp;pin fan</dt>
          <dd>
            GPIO&nbsp;18 to a MOSFET module’s signal input, fan on its output, 12&nbsp;V
            to the module. Switching the low side. Again: common ground, and no
            fan current through the ESP32.
          </dd>
        </dl>
        <p className="dim small">
          The strip listens for E1.31 on universe 1, DMX address 1, three
          channels. It goes dark after five seconds of silence: a light is not
          a hazard the way a fan is, so it holds through a network stumble
          rather than flickering, but it does not sit lit for ever after the
          conductor has gone.
        </p>
        <h3 className="adm-sub">Which pins can do what</h3>
        <p className="dim small">
          Most GPIOs will drive any of the three device types. These will not,
          and the last group is the one to be careful with: a configuration that
          claims one of those can leave a board that will not boot, and the only
          way back is USB.
        </p>
        <div className="adm-scroll">
          <table className="adm-table">
            <thead>
              <tr><th>Pins</th><th>Why not</th></tr>
            </thead>
            <tbody>
              <tr>
                <td><code>34&ndash;39</code></td>
                <td>Input only. No output of any kind, from the chip’s own
                  <code> SOC_GPIO_VALID_OUTPUT_GPIO_MASK</code>.</td>
              </tr>
              <tr>
                <td><code>6&ndash;11</code></td>
                <td>The SPI flash. The chip calls them valid; using them stops
                  the board running.</td>
              </tr>
              <tr>
                <td><code>1</code>, <code>3</code></td>
                <td>The console UART, where Wi-Fi provisioning lives. Taking
                  them removes the way back in.</td>
              </tr>
              <tr>
                <td><code>0</code>, <code>2</code>, <code>12</code>, <code>15</code></td>
                <td>Strapping pins, read at boot. Usable with care;
                  <code> 12</code> held high at boot can leave a board that will
                  not start.</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="dim small">
          And there is a limit on how many, not just which. The ESP32 has
          <strong> 8 RMT channels</strong>, so at most eight addressable strips,
          and <strong>8 PWM channels across 4 timers</strong>, so at most eight
          dimmed outputs at no more than four distinct frequencies. Three or
          four devices on one board is comfortable; ten is not.
        </p>

        <p className="adm-warn small">
          The declared spin up time in the firmware is 1.8&nbsp;s and it is a guess.
          Measure yours and put the real number in, because the conductor fires
          every cue that far ahead and a lie there makes the whole rig feel wrong
          in a way that is hard to diagnose from the room.
        </p>
      </section>
    </div>
  );
}
