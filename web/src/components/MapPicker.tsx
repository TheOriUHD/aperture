import { useEffect, useRef } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";

// A teardrop pin drawn inline so we don't depend on Leaflet's bundled marker
// PNGs (which break under Vite's asset hashing). Filled with currentColor so it
// takes the accent token from its class.
const pinIcon = L.divIcon({
  className: "",
  html:
    '<svg width="30" height="30" viewBox="0 0 24 24" class="text-brand" fill="currentColor" stroke="white" stroke-width="1.4" ' +
    'style="filter:drop-shadow(0 1px 2px rgba(0,0,0,.5))">' +
    '<path d="M12 2C8.1 2 5 5.1 5 9c0 5.2 7 13 7 13s7-7.8 7-13c0-3.9-3.1-7-7-7z"/>' +
    '<circle cx="12" cy="9" r="2.6" fill="white" stroke="none"/></svg>',
  iconSize: [30, 30],
  iconAnchor: [15, 30],
});

// MapPicker shows an OpenStreetMap map with a draggable pin. Dragging or clicking
// reports the new coordinates; external lat/lon changes (from search) recenter it.
export function MapPicker({
  lat,
  lon,
  onChange,
}: {
  lat: number;
  lon: number;
  onChange: (lat: number, lon: number) => void;
}) {
  const elRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<L.Map | null>(null);
  const markerRef = useRef<L.Marker | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!elRef.current || mapRef.current) return;
    const hasLoc = !!(lat || lon);
    const map = L.map(elRef.current, { zoomControl: true }).setView(
      [lat || 20, lon || 0],
      hasLoc ? 12 : 2,
    );
    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19,
      attribution: "© OpenStreetMap",
    }).addTo(map);

    const marker = L.marker([lat || 20, lon || 0], { draggable: true, icon: pinIcon }).addTo(map);
    const report = (p: L.LatLng) =>
      onChangeRef.current(Number(p.lat.toFixed(5)), Number(p.lng.toFixed(5)));
    marker.on("dragend", () => report(marker.getLatLng()));
    map.on("click", (e: L.LeafletMouseEvent) => {
      marker.setLatLng(e.latlng);
      report(e.latlng);
    });

    mapRef.current = map;
    markerRef.current = marker;
    // Container may be measured before layout settles; nudge Leaflet to re-read.
    setTimeout(() => map.invalidateSize(), 0);
    return () => {
      map.remove();
      mapRef.current = null;
      markerRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // External coordinate change (e.g. picking a search result): move + recenter.
  useEffect(() => {
    const map = mapRef.current;
    const marker = markerRef.current;
    if (!map || !marker || !(lat || lon)) return;
    marker.setLatLng([lat, lon]);
    map.setView([lat, lon], Math.max(map.getZoom(), 12), { animate: true });
  }, [lat, lon]);

  // The OSM tiles are drawn dark (inverted, hue-rotated back) and Leaflet's
  // white controls restyled, so the map sits in the dark page instead of glaring
  // out of it. The pin lives in the marker pane and keeps its true colour.
  return (
    <div
      ref={elRef}
      className={[
        "z-0 h-64 w-full overflow-hidden rounded-lg border border-line !bg-ink-850",
        "[&_.leaflet-tile-pane]:[filter:invert(1)_hue-rotate(180deg)_brightness(0.92)_contrast(0.88)_saturate(0.55)]",
        "[&_.leaflet-bar]:!border-line-strong [&_.leaflet-bar]:!shadow-none",
        "[&_.leaflet-bar_a]:!border-line-strong [&_.leaflet-bar_a]:!bg-ink-800 [&_.leaflet-bar_a]:!text-fg [&_.leaflet-bar_a:hover]:!bg-ink-700",
        "[&_.leaflet-control-attribution]:!bg-ink-900/75 [&_.leaflet-control-attribution]:!text-fg-faint",
        "[&_.leaflet-control-attribution_a]:!text-fg-muted",
      ].join(" ")}
    />
  );
}
