import type { MenuDataItem } from '@ant-design/pro-layout';
import { getMenu, getRouters, getService } from './request';

const queryRouters = async (name: string): Promise<MenuDataItem[]> => {
  const mdItem: MenuDataItem[] = [];

  const data = await getRouters(name);
  console.log(data);
  if (data && data.success && data.data) {
    console.log(1);
    const menus = data.data;
    let servcie = '';
    let mitem: any;
    menus.forEach((item: any) => {
      console.log(2);
      if (item.ServiceName != servcie) {
        servcie = item.ServiceName;
        mitem = { path: '/' + servcie, name: servcie, routes: [] };
        mdItem.push(mitem);
      }
      if (mitem) {
        const r = mitem.routes.find((route: any) => {
          return route.name == item.InstanceName;
        });
        if (!r) {
          mitem.routes.push({
            path: '/main/' + servcie + '/' + item.InstanceName.toLowerCase(),
            name: item.InstanceName,
            exact: true,
          });
        }
      }
    });
  }

  return mdItem;
};

export const GetMenuItem = async (): Promise<MenuDataItem[]> => {
  let mdItem: MenuDataItem[] = [];
  const menuData = await getDir('');
  console.log(menuData);
  mdItem = [...mdItem, ...menuData];
  return mdItem;
  // const result = await getService();
  // let mdItem: MenuDataItem[] = [];
  // console.log(result);
  // if (result) {
  //   for (let i = 0; i < result.length; i++) {
  //     const item = result[i];
  //     if (item.Name == "server") {
  //       const menuData = await queryRouters(item.viewmanage);
  //       console.log(menuData);
  //       mdItem = [...mdItem, ...menuData];
  //     } else {
  //       const menuData = await getDir(item.Name);
  //       console.log(menuData);
  //       mdItem = [...mdItem, ...menuData];
  //       return mdItem;
  //     }
  //   }
  // }
  // console.log(mdItem);
  // return mdItem;
};

async function getDir(service: string): Promise<MenuDataItem[]> {
  const mdItem: MenuDataItem[] = [];
  const dirs = await getMenu(service);
  if (dirs && dirs.success && dirs.data) {
    dirs.data.forEach((item: any) => {
      const meuns: MenuDataItem[] = [];
      if (item.menuitems && item.menuitems.length > 0) {
        item.menuitems.forEach((childItem: any) => {
          const path = childItem.url.replace('/api/manage/', '/main/');
          meuns.push({
            name: childItem.title ?? childItem.name,
            path: path,
            exact: true,
          });
        });
      }
      mdItem.push({
        name: item.title ?? item.name,
        path: '/' + item.name,
        icon: item.icon,
        routes: meuns,
      });
    });
  }
  return mdItem;
}
