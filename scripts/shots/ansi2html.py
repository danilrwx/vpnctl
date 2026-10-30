import sys, html, re
# ansi2html.py <in.txt> <out.html> <title> — ANSI SGR → HTML (тёмная тема, как в tmux)
PAL = ['#0e1116','#f85149','#3fb950','#d29922','#58a6ff','#bc8cff','#39c5cf','#c9d1d9',
       '#6e7681','#ff7b72','#56d364','#e3b341','#79c0ff','#d2a8ff','#56d4dd','#f0f6fc']
def c256(n):
    if n < 16: return PAL[n]
    if n < 232:
        n -= 16; r, g, b = n//36, (n//6)%6, n%6
        return '#%02x%02x%02x' % tuple(0 if v == 0 else 55+v*40 for v in (r,g,b))
    v = 8 + (n-232)*10; return '#%02x%02x%02x' % (v,v,v)
# box-символы → CSS-клетки: линии и скругления стыкуются точно при любом шрифте
BOX = {'─':'lr','│':'ud','┌':'dr','┐':'dl','└':'ur','┘':'ul','╭':'dr r','╮':'dl r','╰':'ur r','╯':'ul r',
       '├':'udr','┤':'udl','┬':'dlr','┴':'ulr','┼':'udlr','╌':'lr','┈':'lr'}
def boxes(s):
    out=[]
    for ch in s:
        if ch in BOX:
            cls=' '.join('b'+c for c in BOX[ch].split(' ')[0]) + (' rd' if BOX[ch].endswith(' r') else '')
            out.append(f'<i class="b {cls}"></i>')
        else:
            out.append(ch)
    return ''.join(out)
src = open(sys.argv[1], encoding='utf-8', errors='replace').read()
out = []; st = dict(fg=None, bg=None, b=False, d=False, rev=False)
def span(t):
    if not t: return ''
    fg, bg = st['fg'], st['bg']
    if st['rev']: fg, bg = (bg or '#0e1116'), (fg or '#c9d1d9')
    css = []
    if fg: css.append(f'color:{fg}')
    if bg: css.append(f'background:{bg}')
    if st['b']: css.append('font-weight:700')
    if st['d']: css.append('opacity:.6')
    e = boxes(html.escape(t))
    return f'<span style="{";".join(css)}">{e}</span>' if css else e
for line in src.split('\n'):
    pos = 0
    for m in re.finditer(r'\x1b\[([0-9;]*)m', line):
        out.append(span(line[pos:m.start()])); pos = m.end()
        p = [int(x) if x else 0 for x in m.group(1).split(';')] or [0]
        i = 0
        while i < len(p):
            c = p[i]
            if c == 0: st.update(fg=None, bg=None, b=False, d=False, rev=False)
            elif c == 1: st['b'] = True
            elif c == 2: st['d'] = True
            elif c == 7: st['rev'] = True
            elif c == 22: st['b'] = st['d'] = False
            elif c == 27: st['rev'] = False
            elif 30 <= c <= 37: st['fg'] = PAL[c-30]
            elif 90 <= c <= 97: st['fg'] = PAL[c-90+8]
            elif 40 <= c <= 47: st['bg'] = PAL[c-40]
            elif 100 <= c <= 107: st['bg'] = PAL[c-100+8]
            elif c == 39: st['fg'] = None
            elif c == 49: st['bg'] = None
            elif c in (38, 48) and i+1 < len(p):
                key = 'fg' if c == 38 else 'bg'
                if p[i+1] == 5 and i+2 < len(p): st[key] = c256(p[i+2]); i += 2
                elif p[i+1] == 2 and i+4 < len(p): st[key] = '#%02x%02x%02x' % (p[i+2], p[i+3], p[i+4]); i += 4
            i += 1
    out.append(span(line[pos:])); out.append('\n')
body = ''.join(out).rstrip('\n')
open(sys.argv[2], 'w').write(f'''<!doctype html><meta charset="utf-8"><title>{html.escape(sys.argv[3])}</title>
<style>body{{margin:0;background:#0e1116}} .w{{display:inline-block;background:#0e1116;color:#c9d1d9;font:14px/20px "DejaVu Sans Mono",monospace;padding:14px 18px;border-radius:10px;white-space:pre}}
.b{{display:inline-block;width:1ch;height:20px;vertical-align:top;position:relative;background-repeat:no-repeat}}
.b.bl{{background-image:linear-gradient(currentColor,currentColor);background-size:calc(50% + 1px) 2px;background-position:0 9px}}
.b.br{{background-image:linear-gradient(currentColor,currentColor);background-size:calc(50% + 1px) 2px;background-position:100% 9px}}
.b.bl.br{{background-size:100% 2px;background-position:0 9px}}
.b.bu::before,.b.bd::before,.b.bu.bd::before{{content:"";position:absolute;left:calc(50% - 1px);width:2px;background:currentColor}}
.b.bu::before{{top:0;height:11px}} .b.bd::before{{top:9px;height:11px}} .b.bu.bd::before{{top:0;height:20px}}
.b.rd{{background-image:none}} .b.rd::before{{display:none}}
.b.rd::after{{content:"";position:absolute;box-sizing:border-box;width:calc(50% + 1px);height:11px;border:0 solid currentColor}}
.b.rd.bd.br::after{{left:calc(50% - 1px);top:9px;border-top-width:2px;border-left-width:2px;border-top-left-radius:6px}}
.b.rd.bd.bl::after{{right:calc(50% - 1px);top:9px;border-top-width:2px;border-right-width:2px;border-top-right-radius:6px}}
.b.rd.bu.br::after{{left:calc(50% - 1px);top:0;border-bottom-width:2px;border-left-width:2px;border-bottom-left-radius:6px}}
.b.rd.bu.bl::after{{right:calc(50% - 1px);top:0;border-bottom-width:2px;border-right-width:2px;border-bottom-right-radius:6px}}
</style>
<div class="w">{body}</div>''')
