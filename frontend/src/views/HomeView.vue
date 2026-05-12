<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Default Home Page -->
  <div
    v-else
    class="relative flex min-h-screen flex-col overflow-hidden bg-gradient-to-br from-gray-50 via-primary-50/30 to-gray-100 dark:from-dark-950 dark:via-dark-900 dark:to-dark-950"
  >
    <!-- Background Decorations -->
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div
        class="absolute -right-40 -top-40 h-96 w-96 rounded-full bg-primary-400/20 blur-3xl"
      ></div>
      <div
        class="absolute -bottom-40 -left-40 h-96 w-96 rounded-full bg-primary-500/15 blur-3xl"
      ></div>
      <div
        class="absolute left-1/3 top-1/4 h-72 w-72 rounded-full bg-primary-300/10 blur-3xl"
      ></div>
      <div
        class="absolute bottom-1/4 right-1/4 h-64 w-64 rounded-full bg-primary-400/10 blur-3xl"
      ></div>
      <div
        class="absolute inset-0 bg-[linear-gradient(rgba(176,132,80,0.03)_1px,transparent_1px),linear-gradient(90deg,rgba(176,132,80,0.03)_1px,transparent_1px)] bg-[size:64px_64px]"
      ></div>
    </div>

    <!-- Header -->
    <header class="relative z-20 px-6 py-4">
      <nav class="mx-auto flex max-w-6xl items-center justify-between">
        <!-- Logo -->
        <div class="flex items-center">
          <div class="h-10 w-10 overflow-hidden rounded-xl shadow-md">
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
        </div>

        <!-- Nav Actions -->
        <div class="flex items-center gap-3">
          <!-- Language Switcher -->
          <LocaleSwitcher />

          <!-- Doc Link -->
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>

          <!-- Theme Toggle -->
          <button
            @click="toggleTheme"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
            :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>

          <!-- Login / Dashboard Button -->
          <router-link
            v-if="isAuthenticated"
            :to="dashboardPath"
            class="inline-flex items-center gap-1.5 rounded-full bg-gray-900 py-1 pl-1 pr-2.5 transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
          >
            <span
              class="flex h-5 w-5 items-center justify-center rounded-full bg-gradient-to-br from-primary-400 to-primary-600 text-[10px] font-semibold text-white"
            >
              {{ userInitial }}
            </span>
            <span class="text-xs font-medium text-white">{{ t('home.dashboard') }}</span>
            <svg
              class="h-3 w-3 text-gray-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M4.5 19.5l15-15m0 0H8.25m11.25 0v11.25"
              />
            </svg>
          </router-link>
          <router-link
            v-else
            to="/login"
            class="inline-flex items-center rounded-full bg-gray-900 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-gray-800 dark:bg-gray-800 dark:hover:bg-gray-700"
          >
            {{ t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <!-- Main Content -->
    <main class="relative z-10 flex-1 px-6 py-16">
      <div class="mx-auto max-w-6xl">

        <!-- ===== Section 1: Hero ===== -->
        <div class="mb-20 flex flex-col items-center justify-between gap-12 lg:flex-row lg:gap-16">
          <!-- Left: Text Content -->
          <div class="flex-1 text-center lg:text-left">
            <h1
              class="mb-2 font-serif text-4xl font-bold text-gray-900 dark:text-white md:text-5xl lg:text-6xl"
            >
              {{ t('home.heroTitle') }}
            </h1>
            <p class="mb-4 text-lg font-medium italic text-primary-600 dark:text-primary-400 md:text-xl">
              {{ t('home.heroSubtitle') }}
            </p>
            <p class="mb-8 text-base leading-relaxed text-gray-600 dark:text-dark-300">
              {{ t('home.heroDescription') }}
            </p>

            <!-- Feature Tags -->
            <div class="mb-8 flex flex-wrap items-center justify-center gap-3 lg:justify-start">
              <span
                class="inline-flex items-center gap-2 rounded-full border border-primary-200/50 bg-primary-50/80 px-4 py-1.5 text-xs font-medium text-primary-700 dark:border-primary-800/50 dark:bg-primary-900/20 dark:text-primary-300"
              >
                <Icon name="shield" size="xs" />
                {{ t('home.tags.anthropicNative') }}
              </span>
              <span
                class="inline-flex items-center gap-2 rounded-full border border-gray-200/50 bg-white/80 px-4 py-1.5 text-xs font-medium text-gray-700 dark:border-dark-700/50 dark:bg-dark-800/80 dark:text-dark-200"
              >
                <Icon name="server" size="xs" />
                {{ t('home.tags.multiModel') }}
              </span>
              <span
                class="inline-flex items-center gap-2 rounded-full border border-gray-200/50 bg-white/80 px-4 py-1.5 text-xs font-medium text-gray-700 dark:border-dark-700/50 dark:bg-dark-800/80 dark:text-dark-200"
              >
                <Icon name="bolt" size="xs" />
                {{ t('home.tags.enterprise') }}
              </span>
            </div>

            <!-- CTA Buttons -->
            <div class="flex flex-wrap items-center justify-center gap-4 lg:justify-start">
              <router-link
                :to="isAuthenticated ? dashboardPath : '/login'"
                class="btn btn-primary px-8 py-3 text-base shadow-lg shadow-primary-500/30"
              >
                {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
              </router-link>
              <a
                href="#pricing"
                class="btn btn-secondary px-6 py-3 text-base"
              >
                {{ t('home.viewPlans') }}
              </a>
            </div>
          </div>

          <!-- Right: Terminal Animation (CC CLI style) -->
          <div class="flex flex-1 justify-center lg:justify-end">
            <div class="terminal-container">
              <div class="terminal-window">
                <!-- Window header -->
                <div class="terminal-header">
                  <div class="terminal-buttons">
                    <span class="btn-close"></span>
                    <span class="btn-minimize"></span>
                    <span class="btn-maximize"></span>
                  </div>
                  <span class="terminal-title">claude code</span>
                </div>
                <!-- Terminal content (intentionally English — mimics real CC CLI output) -->
                <div class="terminal-body">
                  <div class="code-line line-1">
                    <span class="code-prompt">$</span>
                    <span class="code-cmd">claude</span>
                    <span class="code-flag">--model</span>
                    <span class="code-url">opus-4.6</span>
                  </div>
                  <div class="code-line line-2">
                    <span class="code-comment">&#x25C9; Initializing Claude Code...</span>
                  </div>
                  <div class="code-line line-3">
                    <span class="code-prompt">&gt;</span>
                    <span class="code-response">Search for latest API pricing</span>
                  </div>
                  <div class="code-line line-4">
                    <span class="code-success">&#x26A1; web_search</span>
                    <span class="code-comment">"API pricing 2026"</span>
                  </div>
                  <div class="code-line line-5">
                    <span class="code-flag">&#x1F4CB; Planning:</span>
                    <span class="code-comment">analyzing 3 sources...</span>
                  </div>
                  <div class="code-line line-6">
                    <span class="code-success">&#x2713; Analysis complete.</span>
                  </div>
                  <div class="code-line line-7">
                    <span class="code-prompt">$</span>
                    <span class="cursor"></span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- ===== Section 2: Product Suite ===== -->
        <div class="mb-20">
          <div class="mb-8 text-center">
            <h2 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">
              {{ t('home.products.title') }}
            </h2>
            <p class="text-sm text-gray-600 dark:text-dark-400">
              {{ t('home.products.subtitle') }}
            </p>
          </div>
          <div class="grid gap-6 md:grid-cols-3">
            <!-- Gateway -->
            <div class="group rounded-2xl border border-primary-200/50 bg-white/60 p-6 backdrop-blur-sm transition-all duration-300 hover:shadow-xl hover:shadow-primary-500/10 dark:border-primary-800/30 dark:bg-dark-800/60">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-primary-600 shadow-lg shadow-primary-500/30 transition-transform group-hover:scale-110">
                <Icon name="shield" size="lg" class="text-white" />
              </div>
              <div class="mb-1 text-xs font-medium uppercase tracking-wider text-primary-600 dark:text-primary-400">{{ t('home.products.gateway.tagline') }}</div>
              <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">{{ t('home.products.gateway.name') }}</h3>
              <p class="mb-4 text-sm text-gray-600 dark:text-dark-400">{{ t('home.products.gateway.description') }}</p>
              <ul class="space-y-2 text-sm text-gray-600 dark:text-dark-400">
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-primary-500" /> {{ t('home.products.gateway.f1') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-primary-500" /> {{ t('home.products.gateway.f2') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-primary-500" /> {{ t('home.products.gateway.f3') }}</li>
              </ul>
            </div>

            <!-- MetaCode -->
            <div class="group rounded-2xl border border-emerald-200/50 bg-white/60 p-6 backdrop-blur-sm transition-all duration-300 hover:shadow-xl hover:shadow-emerald-500/10 dark:border-emerald-800/30 dark:bg-dark-800/60">
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-emerald-500 to-emerald-600 shadow-lg shadow-emerald-500/30 transition-transform group-hover:scale-110">
                <Icon name="bolt" size="lg" class="text-white" />
              </div>
              <div class="mb-1 text-xs font-medium uppercase tracking-wider text-emerald-600 dark:text-emerald-400">{{ t('home.products.metacode.tagline') }}</div>
              <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">{{ t('home.products.metacode.name') }}</h3>
              <p class="mb-4 text-sm text-gray-600 dark:text-dark-400">{{ t('home.products.metacode.description') }}</p>
              <ul class="mb-4 space-y-2 text-sm text-gray-600 dark:text-dark-400">
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-emerald-500" /> {{ t('home.products.metacode.f1') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-emerald-500" /> {{ t('home.products.metacode.f2') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-emerald-500" /> {{ t('home.products.metacode.f3') }}</li>
              </ul>
              <a href="https://metacode.pages.dev/" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1.5 text-sm font-medium text-emerald-600 transition-colors hover:text-emerald-700 dark:text-emerald-400 dark:hover:text-emerald-300">
                {{ t('home.products.metacode.cta') }}
                <Icon name="arrowRight" size="xs" />
              </a>
            </div>

            <!-- MetaWork -->
            <div class="group relative rounded-2xl border border-gray-200/50 bg-white/60 p-6 backdrop-blur-sm transition-all duration-300 dark:border-dark-700/50 dark:bg-dark-800/60">
              <span class="absolute right-4 top-4 rounded-full border border-purple-200 bg-purple-50 px-3 py-0.5 text-[10px] font-semibold text-purple-600 dark:border-purple-800/50 dark:bg-purple-900/20 dark:text-purple-400">
                {{ t('home.products.metawork.badge') }}
              </span>
              <div class="mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-purple-500 to-purple-600 shadow-lg shadow-purple-500/30 transition-transform group-hover:scale-110">
                <Icon name="cube" size="lg" class="text-white" />
              </div>
              <div class="mb-1 text-xs font-medium uppercase tracking-wider text-purple-600 dark:text-purple-400">{{ t('home.products.metawork.tagline') }}</div>
              <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">{{ t('home.products.metawork.name') }}</h3>
              <p class="mb-4 text-sm text-gray-600 dark:text-dark-400">{{ t('home.products.metawork.description') }}</p>
              <ul class="space-y-2 text-sm text-gray-500 dark:text-dark-500">
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-purple-400" /> {{ t('home.products.metawork.f1') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-purple-400" /> {{ t('home.products.metawork.f2') }}</li>
                <li class="flex items-start gap-2"><Icon name="check" size="xs" class="mt-0.5 shrink-0 text-purple-400" /> {{ t('home.products.metawork.f3') }}</li>
              </ul>
            </div>
          </div>
        </div>

        <!-- ===== Section 3: Pricing ===== -->
        <div id="pricing" class="mb-20 scroll-mt-20">
          <div class="mb-8 text-center">
            <h2 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">
              {{ t('home.pricing.title') }}
            </h2>
            <p class="text-sm text-gray-600 dark:text-dark-400">
              {{ t('home.pricing.subtitle') }}
            </p>
          </div>

          <!-- Billing mode comparison -->
          <div class="mx-auto mb-10 grid max-w-3xl gap-4 md:grid-cols-2">
            <!-- Pay-as-you-go -->
            <div class="rounded-2xl border border-gray-200/50 bg-white/60 p-5 backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/60">
              <div class="mb-2 flex items-center gap-2">
                <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-900/30">
                  <Icon name="creditCard" size="sm" class="text-blue-600 dark:text-blue-400" />
                </div>
                <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('home.pricing.paygo.title') }}</h4>
              </div>
              <p class="mb-2 text-sm text-gray-600 dark:text-dark-400">{{ t('home.pricing.paygo.desc') }}</p>
              <span class="inline-block rounded-full bg-blue-50 px-2.5 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/20 dark:text-blue-400">
                {{ t('home.pricing.paygo.badge') }}
              </span>
            </div>
            <!-- Subscription -->
            <div class="rounded-2xl border border-primary-200/50 bg-gradient-to-br from-primary-50/50 to-white/60 p-5 backdrop-blur-sm dark:border-primary-800/30 dark:from-primary-900/10 dark:to-dark-800/60">
              <div class="mb-2 flex items-center gap-2">
                <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-primary-100 dark:bg-primary-900/30">
                  <Icon name="bolt" size="sm" class="text-primary-600 dark:text-primary-400" />
                </div>
                <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('home.pricing.subscription.title') }}</h4>
              </div>
              <p class="mb-2 text-sm text-gray-600 dark:text-dark-400">{{ t('home.pricing.subscription.desc') }}</p>
              <span class="inline-block rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-400">
                {{ t('home.pricing.subscription.badge') }}
              </span>
            </div>
          </div>
          <div class="grid gap-5 md:grid-cols-2 lg:grid-cols-4">
            <div
              v-for="plan in plans"
              :key="plan.key"
              class="relative rounded-2xl p-6 backdrop-blur-sm transition-all duration-300"
              :class="plan.popular
                ? 'border-2 border-primary-400 bg-white/70 shadow-lg shadow-primary-500/10 hover:shadow-xl dark:border-primary-600 dark:bg-dark-800/70'
                : 'border border-gray-200/50 bg-white/60 hover:shadow-lg dark:border-dark-700/50 dark:bg-dark-800/60'"
            >
              <span
                v-if="plan.popular"
                class="absolute -top-3 left-1/2 -translate-x-1/2 rounded-full bg-gradient-to-r from-primary-500 to-primary-600 px-3 py-0.5 text-xs font-medium text-white shadow"
              >
                {{ t('home.pricing.popular') }}
              </span>
              <h3 class="mb-1 text-lg font-semibold text-gray-900 dark:text-white">
                {{ t(`home.pricing.${plan.key}.name`) }}
              </h3>
              <p class="mb-4 text-xs" :class="plan.popular ? 'text-primary-600 dark:text-primary-400' : 'text-gray-500 dark:text-dark-400'">
                {{ t(`home.pricing.${plan.key}.highlight`) }}
              </p>
              <div class="mb-5">
                <span class="text-3xl font-bold text-gray-900 dark:text-white">&yen;{{ t(`home.pricing.${plan.key}.price`) }}</span>
                <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('home.pricing.perMonth') }}</span>
              </div>
              <ul class="mb-6 space-y-2">
                <li
                  v-for="(feature, idx) in planFeatures[plan.key]"
                  :key="idx"
                  class="flex items-start gap-2 text-sm text-gray-600 dark:text-dark-300"
                >
                  <Icon name="check" size="xs" class="mt-0.5 shrink-0" :class="plan.popular ? 'text-primary-500' : 'text-emerald-500'" />
                  {{ feature }}
                </li>
              </ul>
              <router-link
                to="/purchase"
                class="w-full justify-center py-2.5 text-sm"
                :class="plan.popular ? 'btn btn-primary' : 'btn btn-secondary'"
              >
                {{ t('home.pricing.viewDetails') }}
              </router-link>
            </div>
          </div>
        </div>

        <!-- ===== Section 4: Usage Scenarios + Price Comparison ===== -->
        <div class="mb-20">
          <div class="mb-8 text-center">
            <h2 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">
              {{ t('home.scenarios.title') }}
            </h2>
            <p class="text-sm text-gray-600 dark:text-dark-400">
              {{ t('home.scenarios.subtitle') }}
            </p>
          </div>

          <!-- Scenario Cards — large, clear client + model combos -->
          <div class="grid gap-5 md:grid-cols-2 lg:grid-cols-4">
            <div
              v-for="s in scenarios"
              :key="s.key"
              class="rounded-2xl border p-6 backdrop-blur-sm transition-all duration-300"
              :class="s.highlight
                ? 'border-emerald-200 bg-gradient-to-br from-emerald-50/80 to-white/60 shadow-md dark:border-emerald-800/40 dark:from-emerald-900/10 dark:to-dark-800/60'
                : 'border-gray-200/50 bg-white/60 dark:border-dark-700/50 dark:bg-dark-800/60'"
            >
              <span
                class="mb-4 inline-block rounded-full px-3 py-1 text-xs font-semibold"
                :class="s.highlight
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                  : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-400'"
              >{{ t(`home.scenarios.${s.key}.label`) }}</span>
              <div class="mb-1 text-lg font-bold text-gray-900 dark:text-white">
                {{ t(`home.scenarios.${s.key}.client`) }}
              </div>
              <div class="mb-1 text-base font-semibold text-primary-600 dark:text-primary-400">
                + {{ t(`home.scenarios.${s.key}.model`) }}
              </div>
              <p class="text-sm leading-relaxed text-gray-500 dark:text-dark-400">
                {{ t(`home.scenarios.${s.key}.desc`) }}
              </p>
            </div>
          </div>

          <!-- Price Comparison Panel — 3 models vs official pricing -->
          <div class="mt-10 rounded-2xl border border-gray-200/50 bg-white/60 p-6 backdrop-blur-sm dark:border-dark-700/50 dark:bg-dark-800/60 md:p-8">
            <h3 class="mb-6 text-center text-lg font-bold text-gray-900 dark:text-white">
              {{ t('home.scenarios.priceCompare.title') }}
            </h3>
            <div v-if="priceCompareData.length > 0" class="grid gap-6 md:grid-cols-3">
              <div
                v-for="item in priceCompareData"
                :key="item.model"
                class="text-center"
              >
                <!-- Model name -->
                <div class="mb-2 font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ item.model }}</div>
                <!-- Our price -->
                <div class="mb-1 text-2xl font-bold text-primary-600 dark:text-primary-400">
                  {{ formatUsdFromU(item.ourOutput) }}<span class="text-sm font-normal text-gray-400">/MTok</span>
                </div>
                <!-- Official price strikethrough -->
                <div class="mb-2 text-sm text-gray-400 line-through dark:text-dark-500">
                  {{ t('home.scenarios.priceCompare.official') }} {{ formatUsdFromU(item.officialOutput) }}/MTok
                </div>
                <!-- Discount badge -->
                <span class="inline-block rounded-full bg-emerald-100 px-3 py-1 text-sm font-bold text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400">
                  {{ t('home.scenarios.priceCompare.discount') }}
                </span>
                <!-- Best client -->
                <div class="mt-2 text-xs text-gray-500 dark:text-dark-400">
                  {{ t('home.scenarios.priceCompare.bestWith') }} {{ item.bestClient }}
                </div>
              </div>
            </div>
            <div v-else class="py-4 text-center text-sm text-gray-400">
              {{ t('home.scenarios.priceCompare.loading') }}
            </div>
          </div>

          <div class="mt-6 text-center">
            <router-link
              to="/pricing"
              class="inline-flex items-center gap-2 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
            >
              {{ t('home.scenarios.viewPricing') }}
              <Icon name="arrowRight" size="sm" />
            </router-link>
          </div>
        </div>

        <!-- ===== Section 5: About Us ===== -->
        <div class="mb-20 text-center">
          <h2 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">
            {{ t('home.about.title') }}
          </h2>
          <p class="mb-1 text-lg font-semibold text-primary-600 dark:text-primary-400">
            {{ t('home.about.name') }}
            <span class="ml-1 text-sm font-normal text-gray-400 dark:text-dark-500">{{ t('home.about.nameEn') }}</span>
          </p>
          <p class="mx-auto max-w-lg text-sm text-gray-600 dark:text-dark-400">
            {{ t('home.about.description') }}
          </p>
        </div>

        <!-- ===== Section 6: CTA ===== -->
        <div
          class="mb-8 rounded-2xl border border-primary-200/30 bg-gradient-to-r from-primary-50 to-primary-100/50 p-10 text-center dark:border-primary-800/20 dark:from-primary-900/10 dark:to-dark-800/60"
        >
          <h2 class="mb-3 text-2xl font-bold text-gray-900 dark:text-white">
            {{ t('home.cta.title') }}
          </h2>
          <p class="mb-6 text-sm text-gray-600 dark:text-dark-400">
            {{ t('home.cta.description') }}
          </p>
          <router-link
            :to="ctaTarget"
            class="btn btn-primary px-8 py-3 text-base shadow-lg shadow-primary-500/30"
          >
            {{ ctaLabel }}
            <Icon name="arrowRight" size="md" class="ml-2" :stroke-width="2" />
          </router-link>
        </div>

      </div>
    </main>

    <!-- Footer -->
    <footer class="relative z-10 border-t border-gray-200/50 px-6 py-8 dark:border-dark-800/50">
      <div
        class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-4 text-center sm:flex-row sm:text-left"
      >
        <p class="text-sm text-gray-500 dark:text-dark-400">
          &copy; {{ currentYear }} Metask. {{ t('home.footer.allRightsReserved') }}
        </p>
        <div class="flex items-center gap-4">
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-gray-500 transition-colors hover:text-gray-700 dark:text-dark-400 dark:hover:text-white"
          >
            {{ t('home.docs') }}
          </a>
        </div>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import { formatUsdFromU, USD_TO_U } from '@/utils/format'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, tm, rt } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// ── Data-driven card definitions ──
// NOTE: Tailwind class tokens (e.g. 'from-primary-500') appear as full strings in source,
// so the purge scanner matches them correctly even though they're in JS variables.

const scenarios = [
  { key: 'best', highlight: false },
  { key: 'value', highlight: false },
  { key: 'domestic', highlight: true },
  { key: 'enterprise', highlight: false },
] as const

const plans = [
  { key: 'starter', popular: false },
  { key: 'pro', popular: true },
  { key: 'flagship', popular: false },
  { key: 'ultimate', popular: false },
] as const

const planFeatures = computed(() => {
  const result: Record<string, string[]> = {}
  for (const plan of plans) {
    const key: string = `home.pricing.${plan.key}.features`
    const msgs = tm(key)
    // vue-i18n tm() return type is recursively deep (TS2589), type assertion required
    result[plan.key] = Array.isArray(msgs) ? msgs.map((m: unknown) => rt(m as any)) : []
  }
  return result
})

// ── Site settings ──

const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// ── Theme ──

const isDark = ref(document.documentElement.classList.contains('dark'))

// ── External links ──


// ── Auth state ──

const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')

const registrationEnabled = computed(() => appStore.cachedPublicSettings?.registration_enabled ?? true)
const ctaTarget = computed(() => {
  if (isAuthenticated.value) return dashboardPath.value
  return registrationEnabled.value ? '/register' : '/login'
})
const ctaLabel = computed(() => {
  if (isAuthenticated.value) return t('home.goToDashboard')
  return registrationEnabled.value ? t('home.cta.button') : t('home.getStarted')
})
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

const currentYear = computed(() => new Date().getFullYear())

// ── Price comparison data (from pricing API, cached) ──

interface PriceCompareItem {
  model: string
  ourOutput: number
  officialOutput: number
  bestClient: string
}

const priceCompareData = ref<PriceCompareItem[]>([
  {
    model: 'Claude Sonnet 4.6',
    ourOutput: 15 * 0.83 * USD_TO_U,   // $12.45/MTok → U
    officialOutput: 15 * USD_TO_U,      // $15/MTok → U (Anthropic official, 2026-04)
    bestClient: 'Claude Code',
  },
  {
    model: 'Claude Opus 4.6',
    ourOutput: 25 * 0.83 * USD_TO_U,   // $20.75/MTok → U
    officialOutput: 25 * USD_TO_U,      // $25/MTok → U (Anthropic official, 2026-04)
    bestClient: 'Claude Code',
  },
  {
    model: 'MiniMax M2.5',
    ourOutput: 1.1 * 0.83 * USD_TO_U,  // $0.913/MTok → U
    officialOutput: 1.1 * USD_TO_U,     // $1.1/MTok → U (MiniMax official, 2026-04)
    bestClient: 'MetaCode',
  },
])

function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
/* Terminal Container */
.terminal-container {
  position: relative;
  display: inline-block;
}

/* Terminal Window */
.terminal-window {
  width: 420px;
  background: linear-gradient(145deg, #1e293b 0%, #0f172a 100%);
  border-radius: 14px;
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.4),
    0 0 0 1px rgba(255, 255, 255, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
  overflow: hidden;
  transform: perspective(1000px) rotateX(2deg) rotateY(-2deg);
  transition: transform 0.3s ease;
}

.terminal-window:hover {
  transform: perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(-4px);
}

/* Terminal Header */
.terminal-header {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  background: rgba(30, 41, 59, 0.8);
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}

.terminal-buttons {
  display: flex;
  gap: 8px;
}

.terminal-buttons span {
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.btn-close {
  background: #ef4444;
}
.btn-minimize {
  background: #eab308;
}
.btn-maximize {
  background: #22c55e;
}

.terminal-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  font-family: ui-monospace, monospace;
  color: #94a3b8;
  margin-right: 52px;
}

/* Terminal Body */
.terminal-body {
  padding: 20px 24px;
  font-family: ui-monospace, 'Fira Code', monospace;
  font-size: 14px;
  line-height: 2;
}

.code-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  opacity: 0;
  animation: line-appear 0.5s ease forwards;
}

.line-1 {
  animation-delay: 0.3s;
}
.line-2 {
  animation-delay: 0.9s;
}
.line-3 {
  animation-delay: 1.5s;
}
.line-4 {
  animation-delay: 2.1s;
}
.line-5 {
  animation-delay: 2.7s;
}
.line-6 {
  animation-delay: 3.3s;
}
.line-7 {
  animation-delay: 3.8s;
}

@keyframes line-appear {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.code-prompt {
  color: #22c55e;
  font-weight: bold;
}
.code-cmd {
  color: #38bdf8;
}
.code-flag {
  color: #a78bfa;
}
.code-url {
  color: #c49a6c;
}
.code-comment {
  color: #64748b;
  font-style: italic;
}
.code-success {
  color: #22c55e;
  background: rgba(34, 197, 94, 0.15);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.code-response {
  color: #fbbf24;
}

/* Blinking Cursor */
.cursor {
  display: inline-block;
  width: 8px;
  height: 16px;
  background: #22c55e;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  0%,
  50% {
    opacity: 1;
  }
  51%,
  100% {
    opacity: 0;
  }
}

/* Dark mode adjustments */
:deep(.dark) .terminal-window {
  box-shadow:
    0 25px 50px -12px rgba(0, 0, 0, 0.6),
    0 0 0 1px rgba(176, 132, 80, 0.2),
    0 0 40px rgba(176, 132, 80, 0.1),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}
</style>
