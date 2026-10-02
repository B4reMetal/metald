import { mount } from "svelte";
import Page from "./pages/Listen.svelte";
import "./app.css";

mount(Page, { target: document.getElementById("app")! });
