window.onload = function () {
  //<editor-fold desc="Changeable Configuration Block">
  var domain = window.location.protocol + "//" + window.location.hostname;
  if (window.location.port != "") {
    domain = domain + ":" + window.location.port;
  }
  var host = domain + "/api/openapi";
  // the following lines will be replaced by docker/configurator, when it runs in a docker-container
  window.ui = SwaggerUIBundle({
    url: host, //https://petstore.swagger.io/v2/swagger.json
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: "StandaloneLayout",
  });

  //</editor-fold>
};
