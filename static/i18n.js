/* Yomi Reader — i18n (EN + FR). Exposes t(), setLang(), getLang(), I18N. */
(function () {
  'use strict';

  var I18N = {
    en: {
      tagline: 'Read anything. Pick up anywhere.',
      nav_home: 'Home', nav_adult: 'Adult', nav_library: 'Browse', nav_admin: 'Admin', nav_settings: 'Settings',
      language: 'Language', logout: 'Sign out',
      sign_in: 'Sign in', sign_in_sso: 'Continue', username: 'Username', password: 'Password',
      loading: 'Loading…', retry: 'Retry', error_title: 'Something went wrong',
      continue_reading: 'Continue reading', favorites: 'Favorites', recently_added: 'Recently added',
      recently_updated: 'Recently updated', random: 'Random',
      see_all: 'See all', scroll_left: 'Scroll left', scroll_right: 'Scroll right',
      updated: 'Updated {when}', added: 'Added {when}',
      rel_just_now: 'just now', rel_minutes: '{n}m ago', rel_hours: '{n}h ago', rel_days: '{n}d ago',
      empty_lib_title: 'Nothing here yet',
      empty_lib_text: 'The library is empty or still being scanned. New series appear as soon as the scan finds them.',
      empty_adult_title: 'No adult titles yet',
      empty_adult_text: 'Series rated X will show up here. Everything else stays on Home.',
      add_library: 'Add a library',
      ch_short: 'Ch. {n}', p_short: 'p. {n}',
      n_chapters_one: '{n} chapter', n_chapters_other: '{n} chapters', n_chapters: '{n} chapters',
      n_series_one: '{n} series', n_series_other: '{n} series', n_series: '{n} series',
      n_pages: '{n} pages',

      search_ph: 'Search titles…', sort: 'Sort by', order: 'Reverse order', source: 'Source', tags: 'Tags',
      sort_title: 'Title', sort_added: 'Recently added', sort_updated: 'Recently updated', sort_chapters: 'Chapters', sort_year: 'Year', sort_random: 'Random',
      all_sources: 'All sources', all_ages: 'All ages', age_limit: 'Age limit', age_up_to: 'Up to {a}',
      load_more: 'Load more', no_results: 'No matches', no_results_text: 'Try fewer filters or a different search.',
      clear_filters: 'Clear filters', more_tags: '+{n} more', fewer: 'Show fewer',

      continue: 'Continue', start_reading: 'Start reading', read_again: 'Read again', mark_unread: 'Mark as unread', marked_unread: 'Marked as unread',
      favorite: 'Favorite', favorited: 'Favorited', unfavorite: 'Remove favorite',
      sources: 'Sources', chapters: 'Chapters', no_chapters: 'No chapters yet.', also_in: 'Also in',
      oldest_first: 'Oldest first', newest_first: 'Newest first', read: 'Read', age_rating: 'Age rating',
      age_auto: 'Auto-detect', saved: 'Saved',
      change_cover: 'Change cover', cover_upload: 'Upload image',
      cover_from_chapter: 'From a chapter', cover_auto: 'Reset to automatic',
      cover_pick_hint: 'Pick a chapter’s first page, upload your own art, or restore the auto cover.',
      cover_uploading: 'Saving cover…', cover_saved: 'Cover updated',
      cover_chapter_bad: 'That chapter’s first page isn’t a usable image',
      edit_metadata: 'Edit metadata', series_title: 'Title', authors: 'Authors',
      status: 'Status', status_unknown: 'Unknown', year: 'Year', genres: 'Genres', summary: 'Summary',
      genres_ph: 'Action, Fantasy…', tags_ph: 'isekai, revenge…', list_hint: 'Comma-separated',
      title_required: 'Title is required',
      meta_unlock: 'Unlock for rescans',
      meta_unlocked: 'Metadata unlocked for rescans',
      meta_locked_hint: 'These fields are locked — rescans won’t overwrite them from ComicInfo.',

      back_to_series: 'Back to series', chapter: 'Chapter', page: 'Page', page_n: 'Page {n}', reader_mode: 'Reader mode',
      mode_webtoon: 'Webtoon', mode_ltr: 'Left to right', mode_rtl: 'Right to left', mode_vertical: 'Vertical pages', mode_auto: 'Auto (webtoon)',
      prev_chapter: 'Previous chapter', next_chapter: 'Next chapter', fullscreen: 'Fullscreen',
      zone_prev: 'Previous', zone_next: 'Next', zone_menu: 'Menu',
      first_chapter: 'This is the first chapter', last_chapter: 'You are up to date',
      end_of_chapter: 'End of chapter', end_of_series: 'You have reached the latest chapter',
      retry_page: 'Retry page', page_failed: 'Could not load this page', no_pages: 'This chapter has no readable pages',

      tab_libraries: 'Libraries', tab_users: 'Users', tab_settings: 'Settings', tab_jobs: 'Jobs',
      settings_prefs: 'Your preferences', settings_server: 'Server',
      reader_mode_series_hint: 'Reader mode is set per series from the reader toolbar.',
      default_reader_mode_hint: 'Used for series that do not yet have a reader mode of their own.',
      age_note: 'Age ratings are detected automatically. Admins can edit metadata (including age) on each series page.',
      scan: 'Scan', rename: 'Rename', delete: 'Delete', edit: 'Edit', cancel: 'Cancel', close: 'Close', save: 'Save', refresh: 'Refresh', deleted: 'Deleted',
      last_scan: 'Last scan', no_libraries: 'No libraries yet. Add one below.',
      scan_discovering: 'Discovering…', scan_scanning: 'Scanning…', scan_cleanup: 'Cleaning up…',
      scan_done: 'Scan finished: +{a} series, +{c} chapters', scan_error: 'Scan failed', scan_started: 'Scan started',
      confirm_delete_library: 'Remove library “{name}” from Yomi? Your files are not touched.',
      library_name: 'Library name', library_path: 'Folder path', library_path_ph: 'e.g. D:\\manga or /data/manga',
      browse: 'Browse', use_folder: 'Use this folder', drives: 'Drives', no_subfolders: 'No sub-folders.',
      looks_like_library: 'This folder contains a “mangas” directory — looks like a library root.',
      library_added: 'Library added, scanning…',
      add_user: 'Add user', edit_user: 'Edit user', email: 'Email', is_admin: 'Administrator', max_age: 'Max age rating',
      age_desc_E: 'Everyone', age_desc_T: 'Teen', age_desc_M: 'Mature', age_desc_X: 'Explicit',
      libraries: 'Libraries', libraries_hint: 'Leave all unchecked for access to every library.', all_libraries: 'all libraries',
      leave_blank: 'Leave blank to keep', last_seen: 'Last seen', admin_badge: 'admin',
      confirm_delete_user: 'Delete user “{name}”?',
      source_priority: 'Source priority', source_priority_hint: 'Comma-separated, best first. When a chapter exists in several sources, the earliest one wins. Applies instantly.',
      known_sources: 'Known sources (click to add or remove)',
      auto_scan_hours: 'Auto-scan every N hours', auto_scan_hint: '0 turns periodic scans off.',
      default_reader_mode: 'Default reader mode',
      random_refresh: 'Home Random refresh',
      random_refresh_hint: 'How often the Random swimlane picks a new set of series.',
      random_refresh_30m: 'Every 30 minutes', random_refresh_1h: 'Every hour',
      random_refresh_6h: 'Every 6 hours', random_refresh_12h: 'Every 12 hours',
      random_refresh_24h: 'Every 24 hours',
      no_jobs: 'No jobs recorded yet.',
      job_running: 'running', job_done: 'done', job_error: 'error', job_interrupted: 'interrupted'
    },

    fr: {
      tagline: 'Lisez tout. Reprenez partout.',
      nav_home: 'Accueil', nav_adult: 'Adulte', nav_library: 'Parcourir', nav_admin: 'Admin', nav_settings: 'Réglages',
      language: 'Langue', logout: 'Se déconnecter',
      sign_in: 'Se connecter', sign_in_sso: 'Continuer', username: 'Nom d’utilisateur', password: 'Mot de passe',
      loading: 'Chargement…', retry: 'Réessayer', error_title: 'Une erreur est survenue',
      continue_reading: 'Reprendre la lecture', favorites: 'Favoris', recently_added: 'Ajouts récents',
      recently_updated: 'Mises à jour récentes', random: 'Au hasard',
      see_all: 'Tout voir', scroll_left: 'Défiler à gauche', scroll_right: 'Défiler à droite',
      updated: 'Mis à jour {when}', added: 'Ajouté {when}',
      rel_just_now: 'à l’instant', rel_minutes: 'il y a {n} min', rel_hours: 'il y a {n} h', rel_days: 'il y a {n} j',
      empty_lib_title: 'Rien ici pour l’instant',
      empty_lib_text: 'La bibliothèque est vide ou en cours d’analyse. Les séries apparaissent dès que l’analyse les trouve.',
      empty_adult_title: 'Aucun titre adulte pour l’instant',
      empty_adult_text: 'Les séries classées X apparaîtront ici. Le reste reste sur Accueil.',
      add_library: 'Ajouter une bibliothèque',
      ch_short: 'Ch. {n}', p_short: 'p. {n}',
      n_chapters_one: '{n} chapitre', n_chapters_other: '{n} chapitres', n_chapters: '{n} chapitres',
      n_series_one: '{n} série', n_series_other: '{n} séries', n_series: '{n} séries',
      n_pages: '{n} pages',

      search_ph: 'Rechercher un titre…', sort: 'Trier par', order: 'Inverser l’ordre', source: 'Source', tags: 'Étiquettes',
      sort_title: 'Titre', sort_added: 'Ajout récent', sort_updated: 'Mise à jour récente', sort_chapters: 'Chapitres', sort_year: 'Année', sort_random: 'Aléatoire',
      all_sources: 'Toutes les sources', all_ages: 'Tous âges', age_limit: 'Limite d’âge', age_up_to: 'Jusqu’à {a}',
      load_more: 'Charger plus', no_results: 'Aucun résultat', no_results_text: 'Essayez moins de filtres ou une autre recherche.',
      clear_filters: 'Effacer les filtres', more_tags: '+{n} de plus', fewer: 'En voir moins',

      continue: 'Continuer', start_reading: 'Commencer la lecture', read_again: 'Relire', mark_unread: 'Marquer comme non lu', marked_unread: 'Marqué comme non lu',
      favorite: 'Favori', favorited: 'En favori', unfavorite: 'Retirer des favoris',
      sources: 'Sources', chapters: 'Chapitres', no_chapters: 'Aucun chapitre pour l’instant.', also_in: 'Aussi dans',
      oldest_first: 'Plus anciens d’abord', newest_first: 'Plus récents d’abord', read: 'Lu', age_rating: 'Classification d’âge',
      age_auto: 'Détection auto', saved: 'Enregistré',
      change_cover: 'Changer la couverture', cover_upload: 'Envoyer une image',
      cover_from_chapter: 'Depuis un chapitre', cover_auto: 'Couverture automatique',
      cover_pick_hint: 'Choisissez la première page d’un chapitre, envoyez une image, ou restaurez la couverture auto.',
      cover_uploading: 'Enregistrement…', cover_saved: 'Couverture mise à jour',
      cover_chapter_bad: 'La première page de ce chapitre n’est pas une image utilisable',
      edit_metadata: 'Modifier les infos', series_title: 'Titre', authors: 'Auteurs',
      status: 'Statut', status_unknown: 'Inconnu', year: 'Année', genres: 'Genres', summary: 'Résumé',
      genres_ph: 'Action, Fantasy…', tags_ph: 'isekai, vengeance…', list_hint: 'Séparés par des virgules',
      title_required: 'Le titre est obligatoire',
      meta_unlock: 'Déverrouiller pour les analyses',
      meta_unlocked: 'Infos déverrouillées pour les analyses',
      meta_locked_hint: 'Ces champs sont verrouillés — les analyses n’écraseront pas ComicInfo.',

      back_to_series: 'Retour à la série', chapter: 'Chapitre', page: 'Page', page_n: 'Page {n}', reader_mode: 'Mode de lecture',
      mode_webtoon: 'Webtoon', mode_ltr: 'Gauche à droite', mode_rtl: 'Droite à gauche', mode_vertical: 'Pages verticales', mode_auto: 'Auto (webtoon)',
      prev_chapter: 'Chapitre précédent', next_chapter: 'Chapitre suivant', fullscreen: 'Plein écran',
      zone_prev: 'Précédent', zone_next: 'Suivant', zone_menu: 'Menu',
      first_chapter: 'C’est le premier chapitre', last_chapter: 'Vous êtes à jour',
      end_of_chapter: 'Fin du chapitre', end_of_series: 'Vous avez atteint le dernier chapitre',
      retry_page: 'Recharger la page', page_failed: 'Impossible de charger cette page', no_pages: 'Ce chapitre n’a aucune page lisible',

      tab_libraries: 'Bibliothèques', tab_users: 'Utilisateurs', tab_settings: 'Réglages', tab_jobs: 'Tâches',
      settings_prefs: 'Vos préférences', settings_server: 'Serveur',
      reader_mode_series_hint: 'Le mode de lecture se règle par série depuis la barre du lecteur.',
      default_reader_mode_hint: 'Utilisé pour les séries qui n’ont pas encore de mode de lecture.',
      age_note: 'Les classifications d’âge sont détectées automatiquement. Un admin peut modifier les infos (dont l’âge) sur chaque série.',
      scan: 'Analyser', rename: 'Renommer', delete: 'Supprimer', edit: 'Modifier', cancel: 'Annuler', close: 'Fermer', save: 'Enregistrer', refresh: 'Actualiser', deleted: 'Supprimé',
      last_scan: 'Dernière analyse', no_libraries: 'Aucune bibliothèque. Ajoutez-en une ci-dessous.',
      scan_discovering: 'Exploration…', scan_scanning: 'Analyse…', scan_cleanup: 'Nettoyage…',
      scan_done: 'Analyse terminée : +{a} séries, +{c} chapitres', scan_error: 'Échec de l’analyse', scan_started: 'Analyse lancée',
      confirm_delete_library: 'Retirer la bibliothèque « {name} » de Yomi ? Vos fichiers ne sont pas touchés.',
      library_name: 'Nom de la bibliothèque', library_path: 'Chemin du dossier', library_path_ph: 'ex. D:\\manga ou /data/manga',
      browse: 'Parcourir', use_folder: 'Utiliser ce dossier', drives: 'Lecteurs', no_subfolders: 'Aucun sous-dossier.',
      looks_like_library: 'Ce dossier contient un répertoire « mangas » — on dirait une racine de bibliothèque.',
      library_added: 'Bibliothèque ajoutée, analyse en cours…',
      add_user: 'Ajouter un utilisateur', edit_user: 'Modifier l’utilisateur', email: 'E-mail', is_admin: 'Administrateur', max_age: 'Classification max.',
      age_desc_E: 'Tout public', age_desc_T: 'Adolescent', age_desc_M: 'Mature', age_desc_X: 'Explicite',
      libraries: 'Bibliothèques', libraries_hint: 'Ne rien cocher pour accéder à toutes les bibliothèques.', all_libraries: 'toutes les bibliothèques',
      leave_blank: 'Laisser vide pour conserver', last_seen: 'Dernière connexion', admin_badge: 'admin',
      confirm_delete_user: 'Supprimer l’utilisateur « {name} » ?',
      source_priority: 'Priorité des sources', source_priority_hint: 'Séparées par des virgules, la meilleure d’abord. Si un chapitre existe dans plusieurs sources, la première l’emporte. Effet immédiat.',
      known_sources: 'Sources connues (cliquer pour ajouter ou retirer)',
      auto_scan_hours: 'Analyse auto toutes les N heures', auto_scan_hint: '0 désactive les analyses périodiques.',
      default_reader_mode: 'Mode de lecture par défaut',
      random_refresh: 'Rafraîchissement Au hasard',
      random_refresh_hint: 'Fréquence à laquelle la rangée Au hasard tire un nouveau lot de séries.',
      random_refresh_30m: 'Toutes les 30 minutes', random_refresh_1h: 'Toutes les heures',
      random_refresh_6h: 'Toutes les 6 heures', random_refresh_12h: 'Toutes les 12 heures',
      random_refresh_24h: 'Toutes les 24 heures',
      no_jobs: 'Aucune tâche enregistrée.',
      job_running: 'en cours', job_done: 'terminée', job_error: 'erreur', job_interrupted: 'interrompue'
    }
  };

  function detect() {
    var saved = null;
    try { saved = localStorage.getItem('yr_lang'); } catch (e) { /* ignore */ }
    if (saved && I18N[saved]) return saved;
    var nav = (navigator.languages && navigator.languages[0]) || navigator.language || 'en';
    return String(nav).toLowerCase().indexOf('fr') === 0 ? 'fr' : 'en';
  }

  var lang = detect();

  function fmt(s, vars) {
    if (!vars) return s;
    return s.replace(/\{(\w+)\}/g, function (m, k) { return vars[k] != null ? vars[k] : m; });
  }

  // t(key, vars?, count?) — when count is a number, tries key_one / key_other first.
  function t(key, vars, count) {
    var dict = I18N[lang] || I18N.en, s;
    if (typeof count === 'number') {
      var suffix = (lang === 'fr' ? count < 2 : count === 1) ? '_one' : '_other';
      s = dict[key + suffix] || I18N.en[key + suffix];
    }
    if (s == null) s = dict[key];
    if (s == null) s = I18N.en[key];
    if (s == null) return key;
    return fmt(s, vars);
  }

  window.I18N = I18N;
  window.t = t;
  window.getLang = function () { return lang; };
  window.setLang = function (l) {
    if (!I18N[l]) return;
    lang = l;
    try { localStorage.setItem('yr_lang', l); } catch (e) { /* ignore */ }
    document.documentElement.lang = l;
    window.dispatchEvent(new CustomEvent('yr-lang', { detail: l }));
  };
})();
