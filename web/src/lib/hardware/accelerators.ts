// AI / video accelerators an object detector can run on. Aperture does its own
// video work (restreaming, recording, motion) on the CPU; an accelerator takes
// over the object-detection inference, so in the capacity model it carries the
// AI load of cameras that have detection on, instead of the CPU.
//
// aiCameras is how many cameras' motion-triggered detection one device keeps up
// with: detection runs only while there is motion, a few frames a second per
// camera, so a device's sustained inference rate (from its published throughput
// on SSD/YOLO-class models) divided by that.

export type AccelKind = "TPU" | "NPU" | "GPU" | "iGPU" | "VPU";

export interface AccelSpec {
  id: string;
  name: string;
  vendor: string;
  kind: AccelKind;
  tops?: number;
  aiCameras: number;
  video?: string; // hardware video decode/encode, for information
  aliases?: string[];
}

const a = (id: string, name: string, vendor: string, kind: AccelKind, aiCameras: number, tops?: number, video?: string, aliases?: string[]): AccelSpec => ({
  id,
  name,
  vendor,
  kind,
  aiCameras,
  tops,
  video,
  aliases,
});

export const ACCELERATORS: AccelSpec[] = [
  // Google Coral (Edge TPU)
  a("coral-usb", "Google Coral USB Accelerator", "Google", "TPU", 20, 4, undefined, ["edge tpu", "edgetpu"]),
  a("coral-m2", "Google Coral M.2 Accelerator (A+E / B+M key)", "Google", "TPU", 25, 4, undefined, ["edge tpu", "pcie coral"]),
  a("coral-minipcie", "Google Coral Mini PCIe Accelerator", "Google", "TPU", 25, 4, undefined, ["edge tpu"]),
  a("coral-dual", "Google Coral M.2 Accelerator with Dual Edge TPU", "Google", "TPU", 50, 8, undefined, ["edge tpu", "dual tpu"]),
  a("coral-devboard", "Google Coral Dev Board", "Google", "TPU", 20, 4),
  // Hailo
  a("hailo-8", "Hailo-8 (M.2)", "Hailo", "NPU", 90, 26),
  a("hailo-8l", "Hailo-8L (M.2 / Raspberry Pi AI Kit)", "Hailo", "NPU", 45, 13, undefined, ["raspberry pi ai kit", "ai hat"]),
  a("hailo-10h", "Hailo-10H", "Hailo", "NPU", 120, 40),
  a("rpi-ai-hat-26", "Raspberry Pi AI HAT+ (26 TOPS)", "Raspberry Pi", "NPU", 90, 26, undefined, ["hailo-8"]),
  // Built-in NPUs
  a("intel-npu-mtl", "Intel AI Boost NPU (Core Ultra Series 1)", "Intel", "NPU", 30, 11, undefined, ["meteor lake npu"]),
  a("intel-npu-lnl", "Intel AI Boost NPU (Core Ultra Series 2)", "Intel", "NPU", 90, 48, undefined, ["lunar lake npu", "arrow lake npu"]),
  a("amd-xdna", "AMD Ryzen AI NPU (XDNA)", "AMD", "NPU", 30, 16),
  a("amd-xdna2", "AMD Ryzen AI NPU (XDNA 2)", "AMD", "NPU", 90, 50),
  a("rockchip-npu-rk3588", "Rockchip NPU (RK3588 / RK3588S)", "Rockchip", "NPU", 25, 6, "H.265/H.264 8K decode"),
  a("rockchip-npu-rk3568", "Rockchip NPU (RK3568 / RK3566)", "Rockchip", "NPU", 5, 0.8),
  a("amlogic-npu-a311d", "Amlogic NPU (A311D)", "Amlogic", "NPU", 20, 5),
  a("intel-ncs2", "Intel Neural Compute Stick 2", "Intel", "VPU", 8, 1, undefined, ["myriad x", "ncs2"]),
  // Intel integrated graphics (OpenVINO)
  a("intel-uhd-600", "Intel UHD 600 / 605 (Gemini Lake)", "Intel", "iGPU", 4, undefined, "Quick Sync H.264/H.265 decode"),
  a("intel-hd-500", "Intel HD 500 / 505 (Apollo Lake)", "Intel", "iGPU", 3, undefined, "Quick Sync H.264/H.265 decode"),
  a("intel-uhd-n", "Intel UHD (N100 / N150 / N305 class)", "Intel", "iGPU", 12, undefined, "Quick Sync H.264/H.265/AV1 decode", ["alder lake-n", "n100 gpu"]),
  a("intel-uhd-630", "Intel HD / UHD 620–630 (6th–10th gen)", "Intel", "iGPU", 10, undefined, "Quick Sync H.264/H.265 decode"),
  a("intel-uhd-730", "Intel UHD 710–770 (11th–14th gen)", "Intel", "iGPU", 15, undefined, "Quick Sync H.264/H.265/AV1 decode"),
  a("intel-iris-xe", "Intel Iris Xe (11th–13th gen mobile)", "Intel", "iGPU", 25, undefined, "Quick Sync H.264/H.265/AV1 decode"),
  a("intel-arc-igpu", "Intel Arc integrated graphics (Core Ultra)", "Intel", "iGPU", 40, undefined, "Quick Sync H.264/H.265/AV1 decode"),
  // Intel Arc discrete
  a("intel-arc-a310", "Intel Arc A310", "Intel", "GPU", 50, undefined, "H.264/H.265/AV1 encode + decode"),
  a("intel-arc-a380", "Intel Arc A380", "Intel", "GPU", 70, undefined, "H.264/H.265/AV1 encode + decode"),
  a("intel-arc-a750", "Intel Arc A750", "Intel", "GPU", 130, undefined, "H.264/H.265/AV1 encode + decode"),
  a("intel-arc-a770", "Intel Arc A770", "Intel", "GPU", 160, undefined, "H.264/H.265/AV1 encode + decode"),
  a("intel-arc-b580", "Intel Arc B580", "Intel", "GPU", 180, undefined, "H.264/H.265/AV1 encode + decode"),
  // NVIDIA desktop and workstation
  a("nvidia-gt-1030", "NVIDIA GeForce GT 1030", "NVIDIA", "GPU", 12, undefined, "NVDEC H.264/H.265"),
  a("nvidia-gtx-1050-ti", "NVIDIA GeForce GTX 1050 Ti", "NVIDIA", "GPU", 35, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-gtx-1060", "NVIDIA GeForce GTX 1060", "NVIDIA", "GPU", 50, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-gtx-1070", "NVIDIA GeForce GTX 1070", "NVIDIA", "GPU", 65, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-gtx-1080", "NVIDIA GeForce GTX 1080", "NVIDIA", "GPU", 80, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-gtx-1650", "NVIDIA GeForce GTX 1650", "NVIDIA", "GPU", 45, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-gtx-1660", "NVIDIA GeForce GTX 1660 / 1660 Super", "NVIDIA", "GPU", 60, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-rtx-2060", "NVIDIA GeForce RTX 2060", "NVIDIA", "GPU", 90, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-rtx-2070", "NVIDIA GeForce RTX 2070", "NVIDIA", "GPU", 110, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-rtx-2080", "NVIDIA GeForce RTX 2080", "NVIDIA", "GPU", 130, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-rtx-3050", "NVIDIA GeForce RTX 3050", "NVIDIA", "GPU", 80, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-3060", "NVIDIA GeForce RTX 3060", "NVIDIA", "GPU", 120, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-3060-ti", "NVIDIA GeForce RTX 3060 Ti", "NVIDIA", "GPU", 145, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-3070", "NVIDIA GeForce RTX 3070", "NVIDIA", "GPU", 160, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-3080", "NVIDIA GeForce RTX 3080", "NVIDIA", "GPU", 210, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-3090", "NVIDIA GeForce RTX 3090", "NVIDIA", "GPU", 240, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-4060", "NVIDIA GeForce RTX 4060", "NVIDIA", "GPU", 150, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-4060-ti", "NVIDIA GeForce RTX 4060 Ti", "NVIDIA", "GPU", 180, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-4070", "NVIDIA GeForce RTX 4070", "NVIDIA", "GPU", 220, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-4080", "NVIDIA GeForce RTX 4080", "NVIDIA", "GPU", 290, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-4090", "NVIDIA GeForce RTX 4090", "NVIDIA", "GPU", 400, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-5060", "NVIDIA GeForce RTX 5060", "NVIDIA", "GPU", 180, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-5070", "NVIDIA GeForce RTX 5070", "NVIDIA", "GPU", 260, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-5080", "NVIDIA GeForce RTX 5080", "NVIDIA", "GPU", 340, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-5090", "NVIDIA GeForce RTX 5090", "NVIDIA", "GPU", 480, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-t400", "NVIDIA T400", "NVIDIA", "GPU", 30, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-t600", "NVIDIA T600", "NVIDIA", "GPU", 40, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-t1000", "NVIDIA T1000", "NVIDIA", "GPU", 55, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-quadro-p400", "NVIDIA Quadro P400", "NVIDIA", "GPU", 15, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-quadro-p2000", "NVIDIA Quadro P2000", "NVIDIA", "GPU", 50, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-rtx-a2000", "NVIDIA RTX A2000", "NVIDIA", "GPU", 110, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-a4000", "NVIDIA RTX A4000", "NVIDIA", "GPU", 170, undefined, "NVENC/NVDEC H.264/H.265/AV1 decode"),
  a("nvidia-rtx-2000-ada", "NVIDIA RTX 2000 Ada", "NVIDIA", "GPU", 140, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-rtx-4000-ada", "NVIDIA RTX 4000 Ada", "NVIDIA", "GPU", 220, undefined, "NVENC/NVDEC incl. AV1 encode"),
  a("nvidia-tesla-p4", "NVIDIA Tesla P4", "NVIDIA", "GPU", 60, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-tesla-t4", "NVIDIA Tesla T4", "NVIDIA", "GPU", 120, undefined, "NVENC/NVDEC H.264/H.265"),
  a("nvidia-l4", "NVIDIA L4", "NVIDIA", "GPU", 220, undefined, "NVENC/NVDEC incl. AV1 encode"),
  // NVIDIA Jetson (the module's GPU)
  a("jetson-nano", "NVIDIA Jetson Nano (GPU)", "NVIDIA", "GPU", 10, 0.5, "NVDEC H.264/H.265"),
  a("jetson-xavier-nx", "NVIDIA Jetson Xavier NX (GPU + DLA)", "NVIDIA", "GPU", 45, 21, "NVDEC H.264/H.265"),
  a("jetson-orin-nano", "NVIDIA Jetson Orin Nano (GPU)", "NVIDIA", "GPU", 70, 40, "NVDEC H.264/H.265"),
  a("jetson-orin-nx", "NVIDIA Jetson Orin NX (GPU + DLA)", "NVIDIA", "GPU", 110, 100, "NVDEC H.264/H.265"),
  a("jetson-agx-orin", "NVIDIA Jetson AGX Orin (GPU + DLA)", "NVIDIA", "GPU", 220, 275, "NVDEC H.264/H.265"),
  // AMD
  a("amd-radeon-igpu", "AMD Radeon integrated graphics (Vega / 680M / 780M)", "AMD", "iGPU", 20, undefined, "VCN H.264/H.265 decode"),
  a("amd-rx-6400", "AMD Radeon RX 6400", "AMD", "GPU", 35, undefined, "VCN H.264/H.265 decode"),
  a("amd-rx-6600", "AMD Radeon RX 6600", "AMD", "GPU", 70, undefined, "VCN H.264/H.265 encode + decode"),
  a("amd-rx-7600", "AMD Radeon RX 7600", "AMD", "GPU", 90, undefined, "VCN incl. AV1 encode"),
  a("amd-rx-7800-xt", "AMD Radeon RX 7800 XT", "AMD", "GPU", 160, undefined, "VCN incl. AV1 encode"),
];
