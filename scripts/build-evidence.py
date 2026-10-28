# Developer-only import of user-authorized screenshots. The shipped EXE embeds
# sampled UI features; neither Python nor original account screenshots are shipped.
import pathlib,csv,hashlib,json,sys,base64
from PIL import Image
root=pathlib.Path(sys.argv[1]);out=pathlib.Path('internal/evidence/catalog.json')
rows={int(r['编号']):r for r in csv.DictReader((root/'截图索引.csv').open(encoding='utf-8-sig'))}
specs=[
 ('notice','公告',143,[(143,66,102,46),(109,151,160,46)],'notice_enter',(430,467,100,26),True),
 ('tree','动画登录页',150,[(643,75,247,106),(26,18,31,44)],'tree_enter',(715,393,85,27),True),
 ('roles','角色选择',144,[(796,475,108,25),(40,478,84,40)],'enter',(795,477,108,25),True),
 ('game','游戏主界面（身份未核验）',146,[(918,267,26,27),(907,352,36,42)],'menu',(917,308,25,24),True),
 ('menu','菜单展开',148,[(908,470,35,43),(640,297,32,43)],'settings',(912,472,25,25),True),
 ('settings','设置',149,[(131,120,83,25),(122,204,65,25)],'logout',(141,443,31,29),True),
 ('advert','活动广告外框',170,[(738,52,25,25),(197,80,23,23)],'advert_close',(742,56,16,16),False),
 ('talisman','八星打符界面',102,[(304,34,55,26),(316,278,119,25)],'',None,False),
 ('fortune','聚福宝匣',17,[(169,52,99,29),(605,424,142,25)],'',None,False),
 ('stronghold','妖灵据点入口',47,[(71,11,94,25),(760,264,141,21)],'',None,False),
 ('formation','阵法',16,[(168,53,54,27),(657,444,106,22)],'',None,False),
 ('gift','赠送礼物/余额查询',74,[(169,52,99,29),(708,108,40,24)],'',None,False),
 ('market_buy','商行购买',1,[(168,52,51,28),(557,337,25,27)],'',None,False),
 ('market_listing','交易行上架',110,[(167,52,57,29),(604,430,104,24)],'',None,False),
 ('daily','日课',174,[(80,10,46,24),(341,286,67,24)],'',None,False),
 ('cargo','行商货运',89,[(78,9,106,27),(271,397,92,26)],'',None,False),
 ('life_prompt','研习生活技能提示',182,[(286,122,135,28),(279,240,121,29)],'',None,False),
]
def rect(r):return dict(zip(['x','y','w','h'],r))
def sample(im,r):
 x,y,w,h=r;v=[]
 for yy in range(16):
  for xx in range(32):
   R,G,B=im.getpixel((x+xx*(w-1)//31,y+yy*(h-1)//15))
   v.append(((299*R+587*G+114*B)*257//1000)>>8)
 return base64.b64encode(bytes(v)).decode()
entries=[]
for key,label,n,rs,button,bounds,login in specs:
 r=rows[n];im=Image.open(root/r['相对路径']).convert('RGB');anchors=[{'rect':rect(v),'pixels':sample(im,v)} for v in rs]
 entries.append(dict(key=key,label=label,source_id=n,source_sha256=r['SHA256'],page=dict(anchors=anchors,threshold=10),button=button,bounds=rect(bounds) if bounds else None,login=login))
# Only thin, non-identifying gold frame strips are used. Nicknames and avatars
# are excluded; row offsets come from the user's manually tested role centres.
im=Image.open(root/rows[144]['相对路径']).convert('RGB');rs=[(44,64,198,8),(44,123,198,8)]
for i,cy in enumerate([95,169,240,311,382],1):
 anchors=[]
 for r in rs:
  moved=(r[0],r[1]+cy-95,r[2],r[3]);anchors.append({'rect':rect(moved),'pixels':sample(im,r)})
 entries.append(dict(key=f'selected_{i}',label=f'角色{i}选中边框（不证明昵称）',source_id=144,source_sha256=rows[144]['SHA256'],derived='gold frame translated using manually tested row centres',page=dict(anchors=anchors,threshold=10),button=f'role_{i}',bounds=rect((124,cy-10,32,20)),login=True))
icons=[]
for n,job in [(166,'鬼王'),(167,'天音'),(168,'合欢'),(169,'青云')]:
 im=Image.open(root/rows[n]['相对路径']).convert('RGB');icons.append(dict(profession=job,width=im.width,height=im.height,pixels=base64.b64encode(im.tobytes()).decode(),source_id=n,source_sha256=rows[n]['SHA256']))
out.write_text(json.dumps(dict(schema=1,zip_sha256='be45501d1f08d67ae4a76802e086b727081a39dca590a3cd4373a5ddab06ae42',pages=entries,icons=icons),ensure_ascii=False,indent=2)+'\n')
print('Imported',len(entries),'candidate pages and',len(icons),'profession icons;',out.stat().st_size,'bytes. No raw screenshots copied.')
