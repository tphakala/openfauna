import json
import urllib.request
import urllib.parse
import time
import os

langs = ["el", "hu", "is", "it", "lv", "ro", "sl"]
lang_files = {
    "el": "data/locales/el.json",
    "hu": "data/locales/hu.json",
    "is": "data/locales/is.json",
    "it": "data/locales/it.json",
    "lv": "data/locales/lv_lv.json",
    "ro": "data/locales/ro.json",
    "sl": "data/locales/sl.json",
}

def load_locales():
    locales = {}
    for lang, filepath in lang_files.items():
        with open(filepath, "r") as f:
            locales[lang] = json.load(f)
    return locales

def save_locales(locales):
    for lang, filepath in lang_files.items():
        # OpenFauna format: pretty print with indent 2, no escape non-ascii
        with open(filepath, "w", encoding="utf-8") as f:
            json.dump(locales[lang], f, indent=2, ensure_ascii=False, sort_keys=True)
            f.write("\n")

def chunker(seq, size):
    return (seq[pos:pos + size] for pos in range(0, len(seq), size))

def query_wikidata(scientific_names):
    values = " ".join([f'"{name}"' for name in scientific_names])
    query = f"""
    SELECT ?scientificName ?label (LANG(?label) AS ?lang) WHERE {{
      VALUES ?scientificName {{ {values} }}
      ?item wdt:P225 ?scientificName .
      ?item rdfs:label ?label .
      FILTER(LANG(?label) IN ("el", "hu", "is", "it", "lv", "ro", "sl"))
    }}
    """
    url = "https://query.wikidata.org/sparql"
    req = urllib.request.Request(url, data=urllib.parse.urlencode({"query": query}).encode('utf-8'))
    req.add_header("Accept", "application/json")
    req.add_header("User-Agent", "OpenFaunaBot/1.0 (tphakala@koti)")
    
    for _ in range(3):
        try:
            with urllib.request.urlopen(req) as response:
                return json.loads(response.read().decode('utf-8'))
        except Exception as e:
            print(f"Error querying Wikidata: {e}")
            time.sleep(5)
    return None

def main():
    os.chdir("/home/thakala/src/openfauna")
    with open("data/metadata.json", "r") as f:
        metadata = json.load(f)
    
    all_names = list(metadata.keys())
    locales = load_locales()
    
    # Track how many we added
    added = {lang: 0 for lang in langs}
    
    # Process in batches of 300
    batch_size = 300
    print(f"Processing {len(all_names)} species in batches of {batch_size}...")
    
    for i, batch in enumerate(chunker(all_names, batch_size)):
        print(f"Batch {i+1} / {len(all_names)//batch_size + 1}")
        data = query_wikidata(batch)
        if not data:
            print("Failed to fetch batch, skipping.")
            continue
            
        for binding in data["results"]["bindings"]:
            sciname = binding["scientificName"]["value"]
            label = binding["label"]["value"]
            lang = binding["lang"]["value"]
            
            # For Latvian, OpenFauna uses lv_lv
            # The dictionary keys are el, hu, is, it, lv, ro, sl
            if lang in locales:
                # Don't overwrite if it already exists and is not equal to scientific name?
                # Actually, many current entries are just 228 species.
                # Let's add it if it's missing or if we want to overwrite.
                # But wait, if label == sciname, it might be a bad label from Wikidata
                # (some editors use scientific name for missing common name).
                # Actually, Wikidata common practice is to fallback to sciname.
                # Let's just add everything that differs from sciname.
                # If it's exactly the sciname, we still add it so it marks as "covered" 
                # but maybe it's better to omit it so it falls back to English?
                # Actually, English fallback is often better than sciname if it's identical.
                # Let's add it regardless, as OpenFauna likes complete files. Wait.
                # If I don't add identical, we save file size and it falls back to English.
                if label != sciname:
                    if sciname not in locales[lang]:
                        locales[lang][sciname] = label
                        added[lang] += 1
                    else:
                        # If it already exists, only overwrite if we trust wikidata more?
                        # Let's not overwrite existing curated 228 names.
                        pass
        
        # Save every few batches in case of failure
        if i % 5 == 0:
            save_locales(locales)
            
        time.sleep(1) # Be nice to Wikidata
        
    save_locales(locales)
    print("Done!")
    for lang, count in added.items():
        print(f"Added {count} to {lang}")

if __name__ == "__main__":
    main()
