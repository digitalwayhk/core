import { useState } from 'react';

type RouteParams = {
  s?: string;
  c?: string;
};

export default function () {
  const [routeParams, setRouteParams] = useState<RouteParams>({});

  const setRoute = (p: Partial<RouteParams>) => {
    setRouteParams(prev => ({ ...prev, ...p }));
  };

  return {
    routeParams,
    setRouteParams: setRoute,
  };
}
