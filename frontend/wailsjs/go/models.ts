export namespace desktop {
	
	export class CountryInfo {
	    code: string;
	    name: string;
	    flag: string;
	    relay_count: number;
	    active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CountryInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.name = source["name"];
	        this.flag = source["flag"];
	        this.relay_count = source["relay_count"];
	        this.active = source["active"];
	    }
	}
	export class RegistryResponse {
	    countries: CountryInfo[];
	
	    static createFrom(source: any = {}) {
	        return new RegistryResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.countries = this.convertValues(source["countries"], CountryInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StatusResponse {
	    status: string;
	    ip: string;
	    country: string;
	    flag: string;
	    relay_id: string;
	    latency: string;
	    uptime: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.ip = source["ip"];
	        this.country = source["country"];
	        this.flag = source["flag"];
	        this.relay_id = source["relay_id"];
	        this.latency = source["latency"];
	        this.uptime = source["uptime"];
	        this.message = source["message"];
	    }
	}

}

