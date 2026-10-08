#!/usr/bin/env python3
"""Extract the implemented TUI strings from the pinned Clash Verge Rev checkout.
Run from the project root; Python standard library only.
"""
import json,re
from pathlib import Path
root=Path('.reference/clash-verge-rev/src/locales')
langs=['en','ru','zh','fa','tt','id','ar','ko','tr','de','es','jp','zhtw']
sections=['layout','home','shared','profiles','proxies','settings']
def flatten(d,p=''):
 out={}
 for k,v in d.items():
  q=f'{p}.{k}' if p else k
  if isinstance(v,dict):out.update(flatten(v,q))
  elif isinstance(v,str):out[q]=v
 return out
source={l:{k:v for sec in sections for k,v in flatten(json.loads((root/l/f'{sec}.json').read_text()),sec).items()} for l in langs}
en=source['en']; index={}
for k,v in en.items():
 if '{{' not in v:index.setdefault(v,k)
aliases={'TUN Mode':'Tun Mode','DNS Settings':'DNS Settings','Proxy Groups':'Proxies','Proxy Environment':'Copy Env','Network Settings':'Network Settings','Clash Core':'Clash Core','Theme':'Theme Mode','Descriptions':'Descriptions','Subscription URL':'Subscription URL','Test URL':'Test URL','Test':'Delay check','New Profile':'Create Profile','Off':'Disabled','On':'Enabled'}
# Reuse upstream translations, with "Theme" replacing the upstream English name.
for a,v in aliases.items():
 if v in index:index[a]=index[v]
aliases.update({
 'Secret':'Core Secret', 'Stack':'Tun Stack', 'Device (empty = core default)':'Device Name',
 'DNS Hijack (comma separated)':'DNS Hijack', 'Nameservers (comma separated)':'Nameserver',
 'Default Nameservers (comma separated)':'Default Nameserver', 'Fallback Nameservers (comma separated)':'Fallback',
 'Default Latency Test URL (empty = group default)':'Default Latency Test',
 'Default Latency Test Timeout (ms)':'Default Latency Timeout', 'HTTP Port (0 = disabled)':'Http(s) Port',
 'SOCKS Port (0 = disabled)':'Socks Port', 'Mixed Port (HTTP + SOCKS)':'Mixed Port',
 'Diagnostic Information':'Export Diagnostic Info', 'Configuration Directory':'Open Config Dir',
 'Core Directory':'Open Core Dir', 'Logs Directory':'Open Logs Dir',
 'Copy Secret':'Secret copied to clipboard', 'Administrator + Service Mode':'Admin + Service Mode',
 'Proxy Layout Columns (0 = Auto)':'Proxy Layout Columns', 'Last Check Update':'Last Check Update',
 'Settings saved':'Save successfully', 'Profile imported':'Import successfully',
 'Keyboard Shortcuts':'Hotkeys', 'YAML File (optional)':'File',
})
for a,v in aliases.items():
 if v in index:index[a]=index[v]
# Log level labels and ordering follow setting-clash.tsx, including "Warn".
for label in ['debug','info','warning','error','silent']:
 key=f'settings.sections.clash.form.options.logLevel.{label}'
 index[en[key]]=key
for label, key in {
 'Proxy Host': 'settings.modals.sysproxy.fields.proxyHost',
 'Always use Default Bypass': 'settings.modals.sysproxy.fields.alwaysUseDefaultBypass',
 'Proxy Bypass Settings: ': 'settings.modals.sysproxy.fields.proxyBypass',
 'Enable Status': 'settings.modals.sysproxy.fields.enableStatus',
 'Server Addr': 'settings.modals.sysproxy.fields.serverAddr',
 'Bypass': 'settings.modals.sysproxy.fields.bypass',
}.items():
 index[label]=key
texts=set()
for p in Path('internal/tui').glob('*.go'):
 if '_test' not in p.name:
  for raw in re.findall(r'"(?:[^"\\]|\\.)*"',p.read_text()):
   try:texts.add(json.loads(raw))
   except:pass
# Prefixes in status rows are composed with user/core values at runtime.
for v in list(texts):
 if v.endswith(': '):texts.add(v[:-2])
# Keep only strings relevant to the implemented TUI; no web framework is bundled.
for lang in langs:
 out={v:source[lang].get(index[v],en[index[v]]) for v in sorted(texts) if v in index}
 for prefix in ['Enable Status', 'Server Addr', 'Bypass']:
  if prefix in out: out[prefix]=out[prefix].rstrip(' :：')
 if lang=='en':
  out['Theme']='Theme'
  # Directory actions display paths in a terminal rather than opening a GUI.
  for key in ['Configuration Directory','Core Directory','Logs Directory','Diagnostic Information','Copy Secret']:
   if key in out:out[key]=key
 extra=Path('internal/i18n/extra.json')
 if extra.exists():out.update(json.loads(extra.read_text()).get(lang,{}))
 Path(f'internal/i18n/catalogs/{lang}.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print('Upstream strings used:',len(json.loads(Path('internal/i18n/catalogs/en.json').read_text())))
